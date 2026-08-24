# artifact-pipeline

Learning project: CI/CD and platform engineering, end to end, on a local Kubernetes cluster.

## Goal

A local k8s cluster where a `git push` results in a signed, SBOM'd, attested image
that's automatically canary-deployed — with the cluster physically rejecting anything
unsigned, vulnerable, or unattested, and automated metrics deciding promote vs. rollback.

## Phases

1. **Cluster + GitOps skeleton** — kind/k3d cluster, Argo CD, app-of-apps, git push → auto-sync (no manual `kubectl apply`).
2. **CI: build, sign, attest** — GitHub Actions builds image, Syft generates SBOM, cosign signs keylessly (Sigstore/Fulcio/Rekor), SLSA provenance generated, everything pushed to ghcr.io.
3. **Admission control** — Kyverno policies that verify cosign signatures, require valid SLSA provenance, and block critical CVEs (Grype/Trivy) at admission time. Demo: push a bad image, watch it get rejected.
4. **Progressive delivery** — Argo Rollouts + Prometheus, canary steps with automated analysis, auto-rollback on error-rate spikes.
5. **Tie it together + chaos test** — full E2E flow, optional Litmus/Chaos Mesh gate before promotion.

## Known hard parts

- Kyverno's Sigstore/SLSA policy YAML is fiddly.
- Tuning Argo Rollouts analysis thresholds against Prometheus without flapping.
- Making the whole thing reproducible via `make bootstrap`.

## Status

Phase 1 (cluster + Argo CD) not started yet.
