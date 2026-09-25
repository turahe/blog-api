package requests

// AdminCreateUser is POST /api/v1/admin/users. Roles require role.manage.
type AdminCreateUser struct {
	Email    string   `json:"email" binding:"required,email,max=254"`
	Username string   `json:"username" binding:"required,min=3,max=32"`
	FullName string   `json:"fullName" binding:"required,max=120"`
	Password string   `json:"password" binding:"required,min=12,max=128"`
	Roles    []string `json:"roles" binding:"omitempty,max=20,dive,required,max=64"`
}

// AdminResetPassword is POST /api/v1/admin/users/{id}/password/admin-reset (body optional).
type AdminResetPassword struct {
	// RevokeSessions defaults to true.
	RevokeSessions *bool `json:"revokeSessions"`
}
