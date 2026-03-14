#!/bin/bash

printf "%s\n" "Top server ERROR log messages:"

logs="$(
	grep '"level":"ERROR"' 2>/dev/null | \
	jq -Rr 'fromjson? | select(.level=="ERROR" and (.msg | type == "string")) | .msg' 2>/dev/null | \
	sort | uniq -c | sort -rn | head -n 5
)"

if [ -n "$logs" ]; then
	printf "%s\n" "$logs"
else
	printf "%s\n" "No ERROR log messages found or logs unavailable."
fi
