# Skopeo vs Crane

| Use Case | Skopeo | Crane |
|---|---|---|
| **Programmable, can be used as an SDK** | No (CLI only or via `delete := exec.Command("skopeo", "delete"...`) | Yes (CLI + library) |
| **Configuration can be done via API** | No | Yes |
| **Move OCI Artifacts** | Yes (copy + delete) | Yes (copy + delete) |
| **Remapping Artifacts (programmatically)** _( = modify or move image)_ | No | Yes |
| **Image Consistency Verification** (was it really pulled correctly, is it present) | Yes (inspect manifest and compare hashes) | Yes (validate well-formed image + compare digest of image) |
| **Memory and CPU consumption on low end devices, Disk consumption** | "Very lightweight CPU usage" / 37.1MB | "Lightweight CPU usage" / 34.7MB |
| **Dealing with Low bandwidth networks, resume, etc.** | Can report transfer rate and progress per blob, can use `--retry`/`--retry-times` flags, evidence of people using it with [intermittent networking issues](https://github.com/containers/common/issues/654) | No resumable push -> works with single PATCH request, as per [this issue](https://github.com/google/go-containerregistry/issues/1448), can use `.withRetry`, support for [very slow speeds via buffering](https://github.com/google/go-containerregistry/issues/920) / [here](https://github.com/google/go-containerregistry/pull/923) |

## Shared functionalities

Push/Pull/Copy/Delete/Inspect/List Tags in repo

## Exclusive functionalities

### Skopeo

- sync
- generate sigstore
- get manifest digest

### Crane

- catalog repos in a registry
- read individual image blobs
- export filesystem of image
- flatten image layers into single layer
- modify/append image indexes
- get image digest
- filter image indexes by platform
- mutate (modify metadata, entry points, env variables, labels)
- rebase image
- validate that images are well formed
