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

Phases 1-4 done. Phase 5 (tie it together + chaos test) is next.

Phase 3 known issues:

- The `kyverno` Argo CD app shows OutOfSync on 11 `policies.kyverno.io` CRDs while Healthy. Cosmetic; `ignoreDifferences`, chart-value labels and `ServerSideDiff` did not clear it.
- The `critical == 0` and provenance source-repo checks have only been shown to pass. Still to prove they can reject (for example by temporarily requiring `high == 0`).

Phase 4 known issues:

- The canary analysis query must be scoped to the canary's own pods (via the `rollouts-pod-template-hash` label) rather than the whole namespace. A pooled query lets stable-pod traffic dilute a broken canary's error rate, and a broken stable version can block every future canary from passing.
- Prometheus vector results reach Argo's `successCondition` as a slice (`result[0]`, not `result`), and an empty vector needs `or vector(0)` so a zero-error window doesn't read as "no data".
- One canary pod failed its readiness probe once during testing (`connection refused`) and never received traffic, which produced a "no data" analysis error. It did not reproduce on retry; the cause is unconfirmed.
