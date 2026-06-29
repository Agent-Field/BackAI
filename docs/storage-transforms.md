# Storage Transforms

BackAI can transform image objects while serving `GET /api/v1/storage/{key}`.
The original object is not modified. Uploads, signed URLs, and list metadata
continue to operate on the stored object.

Supported source images use Go's standard image decoders:

- PNG
- JPEG
- GIF first frame

Supported output formats:

- `png`
- `jpeg`
- `gif`

## REST

Resize to a fixed width while preserving aspect ratio:

```bash
curl "$AF_STACK_URL/api/v1/storage/images/avatar.png?width=128&format=png" \
  -H "Authorization: Bearer $AF_STACK_API_KEY" \
  --output avatar-128.png
```

Create a thumbnail that fits inside a box:

```bash
curl "$AF_STACK_URL/api/v1/storage/images/avatar.png?transform=thumbnail&width=256&height=256" \
  -H "Authorization: Bearer $AF_STACK_API_KEY" \
  --output avatar-thumb.png
```

`transform=resize` uses exact `width` and `height` when both are set.
`transform=thumbnail` preserves aspect ratio inside the requested box.

## TypeScript

```ts
import { suite } from "@af-stack/sdk"

const png = await suite.storage.download("images/avatar.png", {
  transform: "thumbnail",
  width: 256,
  height: 256,
  format: "png",
})
```

## Limits

- Source object read cap: 32 MiB
- Maximum requested dimension: 4096 px
- Non-image objects return `UNSUPPORTED_TRANSFORM`

For production deployments that need advanced formats, signed URLs,
edge caching, AVIF/WebP, or complex crop policies, put imgproxy or a CDN
image transformer in front of the same storage adapter. The built-in
path covers the self-hosted default and common app thumbnails.
