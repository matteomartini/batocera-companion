#!/bin/bash
# api.sh <path> [json]  — chiama l'API del Companion di test
B=${BASE:-http://localhost:9802}
if [ -n "$2" ]; then curl -s -X POST "$B/api/$1" -H 'content-type: application/json' -d "$2"; else curl -s "$B/api/$1"; fi
