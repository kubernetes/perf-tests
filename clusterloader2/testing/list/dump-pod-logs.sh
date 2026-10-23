#!/bin/bash

set +e

output_dir="${1:?output directory is required}"
namespace="${2:-}"
selector="${3:-group=benchmark}"

mkdir -p "${output_dir}" || exit 0

get_pods_args=(kubectl get pods)
if [ -n "${namespace}" ]; then
  get_pods_args+=( -n "${namespace}" )
else
  get_pods_args+=( --all-namespaces )
fi
if [ -n "${selector}" ]; then
  get_pods_args+=( -l "${selector}" )
fi

pods="$("${get_pods_args[@]}" -o jsonpath='{range .items[*]}{.metadata.namespace}{"\t"}{.metadata.name}{"\n"}{end}' 2>"${output_dir}/kubectl-get.error")"
if [ $? -ne 0 ]; then
  exit 1
fi
rm -f "${output_dir}/kubectl-get.error"

while IFS=$'\t' read -r namespace pod; do
  [ -z "${namespace}" ] || [ -z "${pod}" ] && continue

  kubectl logs \
    -n "${namespace}" \
    "${pod}" \
    --all-containers \
    --prefix \
    > "${output_dir}/${pod}.log" 2>&1 || true

  kubectl logs \
    -n "${namespace}" \
    "${pod}" \
    --all-containers \
    --prefix \
    --previous \
    > "${output_dir}/${pod}.previous.log" 2>&1 || true
done <<< "${pods}"

exit 0
