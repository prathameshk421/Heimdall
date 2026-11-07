---
applies_to: ["internal/bank/**"]
symbols: ["maxBatch"]
triggers: ["maxbatch"]
check: "Is any batch to the bank above 500 lines?"
proof: "Test that Send to a fake bank with 501 lines splits at 500."
kind: incident
source: session 2026-09-14; commit 7ab31c9
---
INC-77: the bank takes at most 500 lines in one request. Above that it answers 200 and silently drops the extra lines, so merchants went unpaid for two days and nothing alerted. Their docs do not say this. Never raise the batch size past 500.
