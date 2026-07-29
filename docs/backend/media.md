# Media Backend Module

## Purpose

The media module handles upload, storage, metadata, dynamic image transformation, cache strategy, and delivery for image assets.

## Storage Requirements

- support Cloudflare R2
- support AWS S3
- support S3-compatible providers such as MinIO and DigitalOcean Spaces
- expose a single storage interface in the application core

## Dynamic Image Delivery

- no pre-generated resized variants are required
- variants are generated when a transform endpoint is requested
- support resize, crop, rotate, quality adjustment, and format conversion
- support optimized delivery in WebP and AVIF where available

## Cache Strategy

Recommended tiers:

- in-memory cache for hot variants
- Redis or equivalent distributed cache for repeated transforms
- optional object-storage-backed cached variants for heavy traffic paths

## Upload Workflow

1. validate file size and mime type
2. read and inspect image metadata
3. scan file for malware
4. store original object
5. persist metadata
6. emit upload event

## Metadata and Tagging

Store:

- original filename
- storage key
- content type
- size
- dimensions
- checksum
- tags

## Failure Rules

- corrupted images return safe validation errors
- invalid transform parameters return client-safe errors
- storage failures return retriable server errors where appropriate
- malware scan failures reject the file before finalization
