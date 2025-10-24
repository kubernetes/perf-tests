#!/bin/bash

expect_completed=$1
buffer_rate=${2:-0.01}
wait_interval=${3:-30}

echo "waiting for $expect_completed Jobs to be completed successfully"
echo "using buffer failure rate: $buffer_rate, wait interval: ${wait_interval}s"

while true; do
    sleep "$wait_interval"

    num_completed=0
    num_failed=0
    num_running=0
    num_pending=0

    # Print table header
    echo "╭─────────────────┬────────────┬──────────┬──────────┬─────────╮"
    echo "│ Namespace       │ Completed  │ Running  │ Pending  │ Failed  │"
    echo "├─────────────────┼────────────┼──────────┼──────────┼─────────┤"

    for ns in $(kubectl get ns --no-headers -o custom-columns=":metadata.name" | grep '^test-'); do
        # Initialize namespace-specific counters
        ns_completed=0
        ns_running=0
        ns_pending=0
        ns_failed=0

        # Get job statuses and count them
        jobs_output=$(kubectl get jobs -n "$ns" --no-headers 2>/dev/null)
        if [[ -n "$jobs_output" ]]; then
            # Process each job line
            while IFS= read -r line; do
                if [[ -n "$line" ]]; then
                    # Parse job fields: NAME STATUS COMPLETIONS DURATION AGE
                    status=$(echo "$line" | awk '{print $2}')
                    completions=$(echo "$line" | awk '{print $3}')

                    # Determine job status based on kubectl output
                    if [[ "$status" == "Complete" ]]; then
                        num_completed=$((num_completed+1))
                        ns_completed=$((ns_completed+1))
                    elif [[ "$status" == "Running" ]]; then
                        # Check if completions field matches pattern X/Y to distinguish running vs pending
                        if [[ "$completions" =~ ^([0-9]+)/([0-9]+)$ ]]; then
                            current=${BASH_REMATCH[1]}
                            total=${BASH_REMATCH[2]}

                            if [[ "$current" -gt 0 && "$current" -lt "$total" ]]; then
                                num_running=$((num_running+1))
                                ns_running=$((ns_running+1))
                            else
                                num_pending=$((num_pending+1))
                                ns_pending=$((ns_pending+1))
                            fi
                        else
                            num_running=$((num_running+1))
                            ns_running=$((ns_running+1))
                        fi
                    elif [[ "$status" == "Failed" ]]; then
                        num_failed=$((num_failed+1))
                        ns_failed=$((ns_failed+1))
                    else
                        num_pending=$((num_pending+1))
                        ns_pending=$((ns_pending+1))
                    fi
                fi
            done <<< "$jobs_output"
        fi

        # Print namespace row in table format
        printf "│ %-15s │ %10s │ %8s │ %8s │ %7s │\n" "$ns" "$ns_completed" "$ns_running" "$ns_pending" "$ns_failed"
    done

    # Print table footer
    echo "╰─────────────────┴────────────┴──────────┴──────────┴─────────╯"
    echo

    echo "Job Status Summary:"
    echo "  Completed: $num_completed"
    echo "  Running: $num_running"
    echo "  Pending: $num_pending"
    echo "  Failed: $num_failed"

    buffer=$(awk -v n="$expect_completed" -v r="$buffer_rate" 'BEGIN{printf "%.0f", n*r}')
    min_completed=$((expect_completed - buffer))
    if (( min_completed < 0 )); then min_completed=0; fi
    echo "Required minimum completed (with buffer): $min_completed (buffer deducted: $buffer)"
    if [[ "$num_completed" -ge "$min_completed" ]]; then
        break;
    fi
done
