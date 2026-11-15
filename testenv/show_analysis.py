import sys, json
j = json.load(sys.stdin); print("status", j["status"], j.get("error"))
r = j.get("result") or {}
for it in r.get("items", []):
    print("%-28s %-6s %-10s %-14s %-8s -> %s  %s  miss=%s" % (it["display_name"], it["confidence"], it["system"], it["core"], it["action"], it["dest_name"], it["notes"], it["missing"]))
print("session", r.get("session"), r.get("summary"))
print("\n".join(j["log"][-6:]))
