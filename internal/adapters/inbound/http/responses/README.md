# Responses

Envelope writers and resource serializers for the HTTP adapter.

| File | Role |
| --- | --- |
| [envelope.go](envelope.go) | `Success` / `Failure` / pagination wrappers |
| [codes.go](codes.go) | Packed response `code` helpers |
| [auth.go](auth.go) | `TokenPair` |
| [posts.go](posts.go) | `Post`, `PostWithTags` |
| [categories.go](categories.go) | `Category` |
| [tags.go](tags.go) | `Tag` |
| [media.go](media.go) | `MediaPresign`, `MediaAsset` |
| [comments.go](comments.go) | `Comment`, `CommentThread` |
| [users.go](users.go) | `User` |

Handlers call `responses.Success(c, status, responses.Post(post))` (etc.).
