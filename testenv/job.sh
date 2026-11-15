#!/bin/bash
# job.sh <jobid> — aspetta la fine di un job e stampa il risultato
B=${BASE:-http://localhost:9802}
for i in $(seq 1 600); do
  j=$(curl -s "$B/api/job?id=$1"); st=$(echo "$j" | python3 -c 'import sys,json; print(json.load(sys.stdin)["status"])')
  [ "$st" != running ] && { echo "$j"; exit 0; }; sleep 0.5
done
echo timeout
