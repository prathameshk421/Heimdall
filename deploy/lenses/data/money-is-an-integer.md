---
applies_to: []
check: "Is every amount an integer in the smallest unit of the currency, and never a float?"
proof: "Add a case with an amount a float cannot represent exactly, and show that the total is still exact."
kind: invariant
source: heimdall lens template: data
status: active
---
A float cannot hold every amount of money. The sum of two amounts that a person expects to be exact can come out a
fraction of a cent smaller, and the difference shows up in a reconciliation days later.

Store money as an integer in the smallest unit of the currency, and say which currency it is. A rate or a percentage
may be a decimal, but the amount that is stored and the amount that is added up may not be.
