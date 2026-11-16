name: build
on:
  pull_request:
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      # ruleid: workflow-action-not-pinned
      - uses: actions/checkout@v4
