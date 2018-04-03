SELECT max(latency_99_percentile) as max_latency_with_cache_99th_percentile
FROM results NATURAL JOIN runs
WHERE dnsmasq_cache IS NOT NULL;
