SELECT
    max(latency_99_percentile) as max_latency_99th_percentile,
    min(latency_99_percentile) as min_latency_99th_percentile
FROM results NATURAL JOIN runs;
