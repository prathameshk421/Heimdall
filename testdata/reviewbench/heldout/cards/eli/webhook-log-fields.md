---
applies_to: ["internal/delivery/**"]
triggers: ["slog.Info", "SendWebhook"]
kind: convention
source: team convention (on-call runbook for delivery logs)
check: "Does every log line written for a delivery attempt use the key delivery_id, spelled exactly that way?"
proof: "Test that SendWebhook logs a 200 reply with a delivery_id key, reading the slog output."
---
The log pipeline indexes delivery logs on the key `delivery_id`. A line under any other key, such as `id`,
cannot be found by the on-call search for a delivery, so the key name is part of the contract and not a
style choice. Every delivery log line uses `delivery_id`.
