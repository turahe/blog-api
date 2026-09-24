# Requests

JSON request bodies and bind/validate helpers for the HTTP adapter.

| File | Role |
| --- | --- |
| [validate.go](validate.go) | `BindJSON`, Laravel-style validation errors |
| [json.go](json.go) | `IsJSONNull` |
| [auth.go](auth.go) | Login, refresh, logout, password |
| [posts.go](posts.go) | Admin create/update/replace-media |
| [categories.go](categories.go) | Admin create category |
| [tags.go](tags.go) | Admin create/update/merge tag |
| [media.go](media.go) | Presign + patch tags |
| [comments.go](comments.go) | Create/edit/flag comment |

Handlers import these types and call `requests.BindJSON(c, &req)`.
