# Eli (consumer team): rules that must hold

- The ledger service lives in a separate repository and parses `refund_ref` from the refund response to key each posting. Removing or renaming `refund_ref` breaks it (INC-031).
- Any change to a field of a response schema in api/openapi.yaml needs a ledger owner's review. `deprecated: true` in the OpenAPI file does not mean the consumer stopped reading the field.
