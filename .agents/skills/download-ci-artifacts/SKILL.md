---
name: download-ci-artifacts
description: Download artifacts from a CI run's GCS bucket to local disk.
---
# Task
Download artifacts from a CI run's GCS bucket to a local directory. Accept a URI and a list of files or patterns to download. Do not decide what to download. Download only what the user requests.

# Workflow
## 1. Parse URI
- **URI normalization**: Accept gcsweb URLs (`https://gcsweb.k8s.io/gcs/...`), raw `gs://` paths, or Prow job URLs (`https://prow.k8s.io/view/gs/...`).
  Convert to GCS paths by replacing `https://gcsweb.k8s.io/gcs/` or `https://prow.k8s.io/view/gs/` with `gs://`.

## 2. Create Local Directory
- **Output path**: If an output directory is provided, use it. Otherwise create a local directory using an identifier extracted from the URI (e.g., the Prow build ID).

## 3. Download
- **Targeted download**: Download only the files or patterns the user specified (e.g., `kube-apiserver.log*`, `*.json`, `junit.xml`).
- **Parallel transfer**: Use `gcloud storage cp` for downloads.

## 4. Report
- **Summary**: List the files downloaded and their sizes.
