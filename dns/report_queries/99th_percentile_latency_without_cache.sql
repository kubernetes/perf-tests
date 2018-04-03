SELECT max(latency_99_percentile) as max_latency_without_cache_99th_percentile
FROM results NATURAL JOIN runs
WHERE dnsmasq_cache IS NULL;
