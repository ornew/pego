#!/bin/sh
# emits a line when an agent transcript has been idle for 120 s (the agent finished)
D=/Users/s27814/.claude/projects/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/subagents
done=""
while true; do
  now=$(date +%s)
  for a in af250488fa23dec67 af976a98d52cc18d1 a2525e05d04659d93 a6e71bccf322b13a9 a7216b408d8ed4776 abc682f25a2658c45; do
    case " $done " in *" $a "*) continue;; esac
    m=$(stat -f %m $D/agent-$a.jsonl)
    if [ $((now - m)) -gt 120 ]; then
      echo "idle: $a"
      done="$done $a"
    fi
  done
  [ $(echo $done | wc -w) -ge 6 ] && break
  sleep 20
done
