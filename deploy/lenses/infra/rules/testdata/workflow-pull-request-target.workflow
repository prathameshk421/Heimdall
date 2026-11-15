name: label
on:
  # ruleid: workflow-pull-request-target
  pull_request_target:
jobs:
  label:
    runs-on: ubuntu-latest
    steps:
      - run: echo labelling
