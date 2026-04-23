#!/bin/bash
here=$(dirname "$(readlink -f "${BASH_SOURCE}")")
cd "$here/../.."
./boxer.sh gov commitdigest extract \
    --token-budget 4096 \
    --since "160d" \
    --resume-dir ./doc/changelog/summaries/gemini31pro \
    --detect-crossings \
    . 2>/dev/null \
| \
./boxer.sh gov commitdigest summarize \
    --llm-endpoint https://generativelanguage.googleapis.com/v1beta/openai \
    --llm-model "models/gemini-3.1-pro-preview" \
    --llm-apikey "$GEMINI_API_KEY" \
    --num-ctx 0 \
    --summaries-dir ./doc/changelog/summaries/gemini31pro
