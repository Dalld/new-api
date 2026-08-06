# Private GHCR Image Publishing Design

## 1. Purpose

Publish a reproducible, private container image for the
`feature/affiliate-commission` branch to GitHub Container Registry (GHCR). The
image is a deployment artifact only; publishing it must not deploy or alter any
running New API service.

## 2. Scope

### Included

- Build the existing `Dockerfile` for `linux/amd64` on every push to
  `feature/affiliate-commission`.
- Write the full commit SHA to the repository `VERSION` file in the ephemeral
  Actions checkout before building, because the Dockerfile embeds that value in
  the application binary.
- Allow a maintainer to run the same build manually with `workflow_dispatch`.
- Push private images to `ghcr.io/dalld/new-api` using the workflow-scoped
  `GITHUB_TOKEN`.
- Publish an immutable full-Git-SHA tag and a rolling branch tag.
- Attach source revision labels, provenance, and an SBOM to each build.
- Use GitHub Actions cache storage to accelerate repeat builds.
- Explicitly exclude `.env`, environment variants, database files, and backup
  directories from the Docker build context before publishing.

### Excluded

- Automatic deployment to any server.
- Building other branches, tags, or architectures.
- Changing the existing Docker Hub release workflow.
- Adding environment files, database dumps, server credentials, or deployment
  credentials to the repository, image, or workflow secrets.
- Automating GHCR package visibility changes through an elevated personal token.

## 3. Publishing Contract

The workflow publishes these references for a commit with full SHA `COMMIT`:

```text
ghcr.io/dalld/new-api:sha-COMMIT
ghcr.io/dalld/new-api:affiliate-commission
```

`sha-COMMIT` is immutable and is the only reference permitted for a deployment
or rollback. `affiliate-commission` is a convenience pointer to the latest
successful branch build and must never be used as a production deployment
reference.

The image is built for `linux/amd64`, which matches the current test server.
The workflow records the full commit SHA in OCI labels so the tag, image
metadata, and Git source can be cross-checked.

## 4. Workflow Design

Add an independent workflow under `.github/workflows/` rather than extending
the existing Docker Hub release workflow. This keeps branch artifacts separate
from official multi-architecture tag releases and prevents a branch push from
publishing Docker Hub release tags.

The workflow has the following properties:

- `on.push.branches` contains only `feature/affiliate-commission`.
- `workflow_dispatch` allows a maintainer to rebuild the checked-out branch.
- `permissions` are limited to `contents: read` and `packages: write`.
- `docker/login-action` logs in to `ghcr.io` with `github.actor` and
  `secrets.GITHUB_TOKEN`.
- `docker/setup-buildx-action` and `docker/build-push-action` build and push
  the existing Dockerfile.
- A preceding shell step writes `${{ github.sha }}` to `VERSION`; this affects
  only the Actions workspace and does not commit a version-file change back to
  the branch.
- GitHub Actions cache is used through `cache-from: type=gha` and
  `cache-to: type=gha,mode=max`.
- Build provenance and an SBOM are enabled.

No server address, pull credential, `.env` value, or database material belongs
in this workflow. Before this workflow is enabled, `.dockerignore` must be
extended to exclude `.env`, `.env.*`, database files, and backup directories,
including application `data/` and `logs/`, from the build context. This is
required because the Dockerfile copies the repository into the build stage; it
must not rely on the current ignore list.

## 5. Package Visibility And Deployment Boundary

The first GHCR package publication must be verified in GitHub Packages as
`Private`. The workflow itself does not make the package public. Deployment
servers will later receive a separate read-only package credential and pull an
explicit `sha-COMMIT` image. That credential and the server Compose image value
are out of scope for this workflow change.

Publishing a new image has no effect on a running server. A future deployment
must explicitly pull the selected immutable image and recreate only the
`new-api` service with `--no-deps`; MySQL and Redis are not deployment targets.

## 6. Validation

Before merging or pushing the workflow change:

1. Parse the YAML and review the trigger, permission, image, and tag values.
2. Confirm `.dockerignore` explicitly excludes `.env`, environment variants,
   database files, backup directories, application `data/`, `logs/`, and
   frontend dependencies from the build context.
3. Push the workflow and verify a successful GitHub Actions run.
4. Confirm the package is private and that both tags resolve to the same image
   digest.
5. Inspect the image labels, provenance, and SBOM from GitHub Packages.
6. Before any server rollout, authenticate with a read-only package credential,
   pull the immutable SHA image, and verify its digest before recreating only
   the application service.

## 7. Failure Handling

If the workflow fails, no deployment is attempted and the last published
immutable image remains available. A failed or partially published rolling tag
is not a deployment signal. If the first package unexpectedly appears public,
stop server integration, set the package to private in GitHub Packages, and
verify visibility before issuing any server pull credential.
