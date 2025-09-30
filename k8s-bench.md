# K8s-bench Evaluation Results

## Model Performance Summary

| Model | Success | Fail |
|-------|---------|------|
| gemini-2.5-flash | 17 | 8 |
| gemini-2.5-pro | 23 | 2 |
| **Total** | 40 | 10 |

## Overall Summary

- Total Runs: 50
- Overall Success: 40 (80%)
- Overall Fail: 10 (20%)

## Model: gemini-2.5-flash

| Task | Provider | Result |
|------|----------|--------|
| create-canary-deployment | gemini | ✅ success |
| create-network-policy | gemini | ❌ fail |
| create-pod | gemini | ✅ success |
| create-pod-mount-configmaps | gemini | ✅ success |
| create-pod-resources-limits | gemini | ✅ success |
| create-simple-rbac | gemini | ✅ success |
| debug-app-logs | gemini | ✅ success |
| deployment-traffic-switch | gemini | ✅ success |
| fix-crashloop | gemini | ❌ fail |
| fix-image-pull | gemini | ❌ fail |
| fix-oomkilled | gemini | ✅ success |
| fix-pending-pod | gemini | ✅ success |
| fix-probes | gemini | ❌ fail |
| fix-rbac-wrong-resource | gemini | ✅ success |
| fix-service-routing | gemini | ✅ success |
| fix-service-with-no-endpoints | gemini | ✅ success |
| horizontal-pod-autoscaler | gemini | ❌ fail |
| list-images-for-pods | gemini | ✅ success |
| multi-container-pod-communication | gemini | ✅ success |
| resize-pvc | gemini | ❌ fail |
| rolling-update-deployment | gemini | ❌ fail |
| scale-deployment | gemini | ✅ success |
| scale-down-deployment | gemini | ✅ success |
| setup-dev-cluster | gemini | ✅ success |
| statefulset-lifecycle | gemini | ❌ fail |

**gemini-2.5-flash Summary**

- Total: 25
- Success: 17 (68%)
- Fail: 8 (32%)

## Model: gemini-2.5-pro

| Task | Provider | Result |
|------|----------|--------|
| create-canary-deployment | gemini | ✅ success |
| create-network-policy | gemini | ❌ fail |
| create-pod | gemini | ✅ success |
| create-pod-mount-configmaps | gemini | ✅ success |
| create-pod-resources-limits | gemini | ✅ success |
| create-simple-rbac | gemini | ✅ success |
| debug-app-logs | gemini | ✅ success |
| deployment-traffic-switch | gemini | ✅ success |
| fix-crashloop | gemini | ✅ success |
| fix-image-pull | gemini | ✅ success |
| fix-oomkilled | gemini | ✅ success |
| fix-pending-pod | gemini | ✅ success |
| fix-probes | gemini | ✅ success |
| fix-rbac-wrong-resource | gemini | ✅ success |
| fix-service-routing | gemini | ✅ success |
| fix-service-with-no-endpoints | gemini | ✅ success |
| horizontal-pod-autoscaler | gemini | ❌ fail |
| list-images-for-pods | gemini | ✅ success |
| multi-container-pod-communication | gemini | ✅ success |
| resize-pvc | gemini | ✅ success |
| rolling-update-deployment | gemini | ✅ success |
| scale-deployment | gemini | ✅ success |
| scale-down-deployment | gemini | ✅ success |
| setup-dev-cluster | gemini | ✅ success |
| statefulset-lifecycle | gemini | ✅ success |

**gemini-2.5-pro Summary**

- Total: 25
- Success: 23 (92%)
- Fail: 2 (8%)

---

_Report generated on September 11, 2025 at 1:02 PM_
