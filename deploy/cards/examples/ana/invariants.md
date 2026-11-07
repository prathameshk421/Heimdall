# Ana (security): rules that must hold

- Every read or write of a money object checks that the caller owns it: `authz.CanAct(caller, obj.MerchantID)`. A lookup by id alone is a hole: merchant B must not read merchant A's payment or refund by guessing an id.
- A missing object and a foreign object answer the same way (404), so ids cannot be probed.
- A test that reads with the owning merchant proves nothing about this rule. Write the cross-merchant case.
