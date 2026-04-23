#!/bin/bash
here=$(dirname "$(readlink -f "${BASH_SOURCE}")")
cd "$here/../.."
./boxer.sh gov commitdigest extract \
    --token-budget 4096 \
    --since "160d" \
    --resume-dir ./doc/changelog/summaries/gemma4_26b \
    --detect-crossings \
    . 2>/dev/null \
  | tee out.json | \
./boxer.sh gov commitdigest summarize \
    --llm-endpoint http://localhost:11434/v1 \
    --llm-model gemma4:26b \
    --num-ctx 8192 \
    --summaries-dir ./doc/changelog/summaries/gemma4_26b
