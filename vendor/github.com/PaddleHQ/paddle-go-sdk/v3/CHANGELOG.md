# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Check our main [developer changelog](https://developer.paddle.com/?utm_source=dx&utm_medium=paddle-go-sdk) for information about changes to the Paddle Billing platform, the Paddle API, and other developer tools.

## 3.0.0 - 2024-12-20

### Fixed

- Limited bytes that can be read via the webhook verification middleware
- Handle nullable `SubscriptionDiscountTimePeriod` property `StartsAt` making it a pointer

### Added

- Support for new API errors
- Vietnamese Dong (VND) currency support, see related [changelog](https://developer.paddle.com/changelog/2024/vietnamese-dong-vnd-supported-currency)
- Simpler creation of full adjustments and a new `AdjustmentType` property, see related [changelog](https://developer.paddle.com/changelog/2024/refund-credit-full-total)
- Support for controlling how Paddle bills when resuming subscriptions with `SubscriptionOnResume`, see related [chagnelog](https://developer.paddle.com/changelog/2024/resume-subscription-billing-period-options)
- Support for non catalog items on Transaction Previews with pointer ID properties

### Changed

- Removed write only property `items` from PricePreview entity
- Removed unused DiscountStatus values `expired` and `used`
- Corrected URL acronym casing for `SubscriptionManagementURLs`

## 2.1.3 - 2024-11-06

### Fixed

- Corrected internal package version number
- Added missing changelog updates

## 2.1.2 - 2024-11-06

### Added

- An example for webhook unmarshalling was added to the package.

## 2.1.1 - 2024-10-18

### Fixed

- Create v2 module for the latest major version

## 2.1.0 - 2024-10-17

### Added

- Support for the Simulation API - see related [changelog entry](https://developer.paddle.com/changelog/2024/webhook-simulator)
- Filtering of customer credit balances by `currency_code` in `AdjustmentsClient.ListCreditBalances`
- New API error mappings

### Fixed

- Subscription resume union funcs now accept the required path param
  - `NewResumeSubscriptionRequestResumeOnASpecificDate`
  - `NewResumeSubscriptionRequestResumeImmediately`
- Response unmarshal for GetNotifications to the Notification struct

## 2.0.0 - 2024-09-18

### Changed

- `PricePreview` has moved to `PreviewPrices` to match API documentation
- Refactored non catalog item types for consistency through the SDK, see [UPGRADING](./UPGRADING.md) for details

### Added

- Support for new Paddle API errors
- Adjustment credit notes (`GetAdjustmentCreditNote`)
- `disposition` query parameter for `GetTransactionInvoice` and `GetAdjustmentCreditNote`
- `active` query parameter filter support for NotificationSettings 
- Subscription update and preview update support non catalog items

### Fixed

- `Type` for Product and Price notifications are nullable `*CatalogType`
- `PaymentMethodID` on Transaction payments is nullable `*PaymentMethodID` for both notifications and API calls

## 1.0.0 - 2024-08-15

No documented changes.

## 0.7.0 - 2024-08-09

### Changed

- Consistency to naming where some occurrences of pluralised prefixes where the pattern is to be singular see [UPGRADING](./UPGRADING.md) for details
- Post consistency fixes to some type names that conflicted, see [UPGRADING](./UPGRADING.md) for details

## 0.6.0 - 2024-08-07

### Changed

- Reports architecture has changed to a oneOf design

### Fixed

- Report filter values incorrectly typed as a `string` when it requires either `string` or `[]string` depending on the filter name. 

## 0.5.0 - 2024-07-26

### Changed

- Paddle API enum types are now correctly enforced where applicable
- Notifications and Events `EventType` property now use `EventTypeName` instead of `string` type
- `Totals` is now a unified type from the previous 4 types `Totals`, `TotalsFormatted`, `UnitTotals` and `UnitTotalsFormatted`
- Various types have been renamed to be less generic and lower the risk of a future breaking change, see [UPGRADING](./UPGRADING.md) for details

### Added

- `Product` has been added as a property of `SubscriptionItem`
- `TaxRatesUsed` has been added as a property of `Adjustments` and `AdjustmentNotification`
- `Notification` property `Type` narrows to `EventTypeName` from `string`
- All notifications and events now have `EventType` narrowed to `EventTypeName` from `string`

### Fixed

- Handling collections for API endpoints without pagination
- `UnitPriceOverride` property `CountryCodes` is now typed as a slice
- `Transaction` property `AvailablePaymentMethods` is now typed as a slice
- `PricePreview` property `AvailablePaymentMethods` is now typed as a slice
- `CreateNotificationSettingRequest` property `SubscribedEvents` is now correctly typed
- `UpdateNotificationSettingRequest` property `SubscribedEvents` is now correctly typed

## 0.4.0 - 2024-06-26

### Changed

- Shared types `TransactionSubscriptionPriceCreateWithProduct` and `TransactionSubscriptionPriceCreateWithProductID` have diverged into 2 new types for Transactions and Subscriptions
- Include entity types are unified with their non-include variant, e.g. `ProductWithIncludes` and `Product` are now just `Product`

### Added

- `AUD` and `CAD` chargeback currencies are available

### Fixed

- Subscription resume properly accepts the subscription id as a path parameter on `ResumeSubscriptionRequest`
- Correctly make a `PATCH` call for `PreviewSubscription` instead of `POST` and use `PatchField` types in the `PreviewSubscriptionRequest` request

## 0.3.0 - 2024-05-27

### Changed

- `Event` and `NotificationsEvent` are now an interfaces.
- `NotificationsEvent` has moved package to `paddlenotification`.

### Added

- New `paddlenotification` package.
- Support for `/events` and `/notifications` endpoints.


## 0.1.0 - 2024-05-07

### Added

- Initial early access release. Added support for the most frequently used Paddle Billing entities and API operations. Check the [README](./README.md) for more information.
