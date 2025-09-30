#!/usr/bin/env bash
set -euo pipefail

# --- Configuration ---
NAMESPACE="statefulset-test"
STS_NAME="db"
EXPECTED_CONTENT="initial_data"

# Verify correct number of replicas
echo "Verifying StatefulSet replica count"
replicas=$(kubectl get sts "${STS_NAME}" -n "${NAMESPACE}" -o jsonpath='{.spec.replicas}')
if [[ "${replicas}" -ne 2 ]]; then
  echo "Expected 2 replicas, but got $replicas"
  exit 1
fi
echo "StatefulSet is scaled to 2 replicas"

echo "Verifying old pods are deleted"
# Wait for scale-down: 2 ready pods and deletion of old pods
kubectl wait pod/db-2 pod/db-3 pod/db-4 -n statefulset-test --for=delete --timeout=120s
echo "Old pods are deleted"

# Verify db-0 and db-1 exist and have the correct data
for pod in db-0 db-1; do
  if ! kubectl get pod "$pod" -n "${NAMESPACE}" &> /dev/null; then
    echo "Pod $pod not found in namespace $NAMESPACE"
    exit 1
  fi

  data=$(kubectl exec "$pod" -n "${NAMESPACE}" -- cat /data/test)
  if [[ "$data" != "${EXPECTED_CONTENT}" ]]; then
    echo "Data missing or incorrect in $pod"
    exit 1
  fi
done

exit 0
