package requests

// CreateRole is POST /api/v1/admin/roles.
type CreateRole struct {
	Name        string   `json:"name" binding:"required,min=2,max=60"`
	Description string   `json:"description" binding:"max=255"`
	Permissions []string `json:"permissions" binding:"omitempty,max=200,dive,required,max=128"`
}

// UpdateRole is PATCH /api/v1/admin/roles/{name}.
type UpdateRole struct {
	Description string `json:"description" binding:"max=255"`
}

// SetRolePermissions is PUT /api/v1/admin/roles/{name}/permissions. An empty list clears the set.
type SetRolePermissions struct {
	Permissions []string `json:"permissions" binding:"required,max=200,dive,required,max=128"`
}

// AssignUserRoles is POST /api/v1/admin/users/{id}/roles.
type AssignUserRoles struct {
	Roles []string `json:"roles" binding:"required,min=1,max=20,dive,required,max=64"`
}
