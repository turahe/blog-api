package requests

// RestoreRevision is POST /api/v1/admin/posts/{id}/revisions/{revision}/restore.
type RestoreRevision struct {
	RestoreNote string `json:"restore_note" binding:"max=1000"`
}
