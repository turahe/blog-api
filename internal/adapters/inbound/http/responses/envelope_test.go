package responses

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSuccessPaginatedLaravelShape(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		page         int
		perPage      int
		total        int64
		wantCurrent  int
		wantLast     int
		wantFrom     *int
		wantTo       *int
		wantPrevNull bool
		wantNextNull bool
	}{
		{
			name:         "first page with more",
			page:         1,
			perPage:      15,
			total:        40,
			wantCurrent:  1,
			wantLast:     3,
			wantFrom:     intPtr(1),
			wantTo:       intPtr(15),
			wantPrevNull: true,
			wantNextNull: false,
		},
		{
			name:         "middle page",
			page:         2,
			perPage:      15,
			total:        40,
			wantCurrent:  2,
			wantLast:     3,
			wantFrom:     intPtr(16),
			wantTo:       intPtr(30),
			wantPrevNull: false,
			wantNextNull: false,
		},
		{
			name:         "empty result",
			page:         1,
			perPage:      15,
			total:        0,
			wantCurrent:  1,
			wantLast:     1,
			wantFrom:     nil,
			wantTo:       nil,
			wantPrevNull: true,
			wantNextNull: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/users?page=2&per_page=15&q=a", nil)
			c.Request.Host = "example.com"

			items := []gin.H{{"id": 1}}
			SuccessPaginated(c, http.StatusOK, items, tt.page, tt.perPage, tt.total)

			require.Equal(t, http.StatusOK, w.Code)
			var envelope Envelope
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
			require.True(t, envelope.OK)
			require.Equal(t, BuildResponseCode(http.StatusOK, ServicePlatform, CaseSuccess), envelope.Code)
			require.NotNil(t, envelope.Links)
			require.NotNil(t, envelope.Links.First)
			require.NotNil(t, envelope.Links.Last)
			require.Equal(t, tt.wantPrevNull, envelope.Links.Prev == nil)
			require.Equal(t, tt.wantNextNull, envelope.Links.Next == nil)
			require.Contains(t, *envelope.Links.First, "page=1")
			require.Contains(t, *envelope.Links.First, "q=a")

			metaBytes, err := json.Marshal(envelope.Meta)
			require.NoError(t, err)
			var meta PaginationMeta
			require.NoError(t, json.Unmarshal(metaBytes, &meta))
			require.Equal(t, tt.wantCurrent, meta.CurrentPage)
			require.Equal(t, tt.wantLast, meta.LastPage)
			require.Equal(t, tt.perPage, meta.PerPage)
			require.Equal(t, tt.total, meta.Total)
			require.Equal(t, "http://example.com/api/v1/users", meta.Path)
			if tt.wantFrom == nil {
				require.Nil(t, meta.From)
			} else {
				require.NotNil(t, meta.From)
				require.Equal(t, *tt.wantFrom, *meta.From)
			}
			if tt.wantTo == nil {
				require.Nil(t, meta.To)
			} else {
				require.NotNil(t, meta.To)
				require.Equal(t, *tt.wantTo, *meta.To)
			}
		})
	}
}

func intPtr(v int) *int { return &v }
