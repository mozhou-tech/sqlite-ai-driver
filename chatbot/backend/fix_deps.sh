#!/bin/bash
cd chatbot/backend
/opt/homebrew/bin/go mod tidy
/opt/homebrew/bin/go mod download github.com/cloudwego/eino-ext/components/model/openai@v0.1.2

