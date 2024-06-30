# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Check our main [developer changelog](https://developer.paddle.com/?utm_source=dx&utm_medium=paddle-go-sdk) for information about changes to the Paddle Billing platform, the Paddle API, and other developer tools.

## 0.4.0 - 

### Breaking changes

- Shared types `TransactionSubscriptionPriceCreateWithProduct` and `TransactionSubscriptionPriceCreateWithProductID` have diverged into 2 new types for Transactions and Subscriptions
- Include entity types are unified with their non-include variant, e.g. `ProductWithIncludes` and `Product` are now just `Product`

### Added

- `AUD` and `CAD` chargeback currencies are available

### Fixed

- Subscription resume properly accepts the subscription id as a path parameter on `ResumeSubscriptionRequest`
- Correctly make a `PATCH` call for `PreviewSubscription` instead of `POST` and use `PatchField` types in the `PreviewSubscriptionRequest` request

## 0.3.0 - 2024-05-27

### Breaking changes

- `Event` and `NotificationsEvent` are now an interfaces.
- `NotificationsEvent` has moved package to `paddlenotification`.

### Added

- New `paddlenotification` package.
- Support for `/events` and `/notifications` endpoints.


## 0.1.0 - 2024-05-07

### Added

- Initial early access release. Added support for the most frequently used Paddle Billing entities and API operations. Check the [README](./README.md) for more information.
