#!/bin/bash
input=$(cat)
user=$(echo "$input" | jq -r '.workspace.current_dir' | sed 's|.*/||')
model=$(echo "$input" | jq -r '.model.display_name')
cwd=$(pwd | sed "s|$HOME|~|")
branch=""
if git rev-parse --git-dir > /dev/null 2>&1; then
  branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null)
fi
remaining_pct=$(echo "$input" | jq -r '.context_window.remaining_percentage // empty')
total_input=$(echo "$input" | jq -r '.context_window.total_input_tokens // 0')
total_output=$(echo "$input" | jq -r '.context_window.total_output_tokens // 0')
total_tokens=$((total_input + total_output))
rl_5h_used=$(echo "$input" | jq -r '.rate_limits.five_hour.used_percentage // empty')
rl_5h_resets=$(echo "$input" | jq -r '.rate_limits.five_hour.resets_at // empty')
rl_7d_used=$(echo "$input" | jq -r '.rate_limits.seven_day.used_percentage // empty')

output=$(printf '\033[0;32m%s\033[0m \033[1;34m%s\033[0m (\033[0;36m%s\033[0m)' "$user" "$cwd" "$model")
[ -n "$branch" ] && output="$output $(printf '\033[0;33m%s\033[0m' "$branch")"
[ "$total_tokens" -gt 0 ] && output="$output [tokens: $total_tokens"
[ -n "$remaining_pct" ] && output="$output | ${remaining_pct}% remaining"
[ "$total_tokens" -gt 0 ] && output="$output]"
if [ -n "$rl_5h_used" ] || [ -n "$rl_7d_used" ]; then
  rl_output="[5h: ${rl_5h_used}%"
  if [ -n "$rl_5h_resets" ]; then
    now=$(date +%s)
    # handle epoch seconds or milliseconds
    if [ "$rl_5h_resets" -gt 9999999999 ] 2>/dev/null; then
      resets_epoch=$(( rl_5h_resets / 1000 ))
    else
      resets_epoch=$rl_5h_resets
    fi
    diff=$(( resets_epoch - now ))
    if [ "$diff" -le 0 ]; then
      resets_fmt="now"
    else
      h=$(( diff / 3600 ))
      m=$(( (diff % 3600) / 60 ))
      if [ "$h" -gt 0 ]; then
        resets_fmt="${h}h${m}m"
      else
        resets_fmt="${m}m"
      fi
    fi
    rl_output="$rl_output resets in $resets_fmt"
  fi
  [ -n "$rl_7d_used" ] && rl_output="$rl_output | 7d: ${rl_7d_used}%"
  rl_output="$rl_output]"
  output="$output $rl_output"
fi
echo "$output"
