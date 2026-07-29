# Glossary

- `RBAC`: role-based access control.
- `Permission`: a named action grant such as `post.publish`.
- `Role`: a collection of permissions assigned to users.
- `Outbox pattern`: a way to store events durably with database changes before publishing them.
- `Watermill`: the event and messaging library used for asynchronous workflows.
- `Published post`: content visible to public API consumers.
- `Moderation`: review flow for comments or user-generated content before approval.
- `High-risk action`: an operation that requires stronger verification, such as disabling 2FA or changing a privileged account.
- `Hexagonal architecture`: a ports-and-adapters architecture that keeps domain logic isolated from infrastructure.
- `R2`: Cloudflare object storage with an S3-compatible API surface.
- `S3-compatible storage`: object storage services that support the S3 API, such as AWS S3, MinIO, and DigitalOcean Spaces.
- `On-the-fly resize`: generating an image variant dynamically at request time instead of storing pre-generated variants.
