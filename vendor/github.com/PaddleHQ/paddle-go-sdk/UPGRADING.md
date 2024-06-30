# Upgrading

All breaking changes prior to v1 will be documented in this file to assist with upgrading.

## v0.4.0

1. This version includes a breaking change to shared types between Transactions and Subscriptions resources which have now been split into their own respective types.   

To upgrade to `0.4.0` from any earlier version you will need to refactor your use of these types as noted below depending on the resource:

| Original Type                                     | Transaction Type                      | Subscription Type                     | 
|---------------------------------------------------|---------------------------------------|---------------------------------------|
| `TransactionSubscriptionPriceCreateWithProduct`   | `TransactionPriceCreateWithProduct`   | `SubscriptionChargeCreateWithProduct` |
| `TransactionSubscriptionPriceCreateWithProductID` | `TransactionPriceCreateWithProductID` | `SubscriptionChargeCreateWithPrice`   |

2. We have unified types that have includes with their non-include variant, e.g. `ProductWithIncludes` and `Product` are now just `Product`

To upgrade to `0.4.0` any references to the types below need to be refactored accordingly

| Previous Type          | New Type            |
|------------------------|---------------------|
| `ProductWithIncludes`  | `Product`           |
| `CustomerIncludes`     | `Customer`          | 
| `TransactionIncludes`  | `Transaction`       |
| `SubscriptionIncludes` | `Subscription`      | 
| `PriceIncludes`        | `Price`             |
