# Upgrading

All breaking changes prior to v1 will be documented in this file to assist with upgrading.

## v0.7.0

1. This release brings consistency to naming. We had some occurrences of pluralised prefixes where the pattern is to be singular. 

As these are changing type definitions you will need to refactor the usage of these types accordingly:

| Previous Type                                      | New Type                                          |
|----------------------------------------------------|---------------------------------------------------|
| `BusinessesContacts`                               | `BusinessContacts`                                |
| `NotificationsEvent`                               | `NotificationEvent`                               |
| `GenericNotificationsEvent`                        | `GenericNotificationEvent`                        |
| `ReportsStatus`                                    | `ReportStatus`                                    |
| `ReportsType`                                      | `ReportType`                                      |
| `ReportsFiltersName`                               | `ReportFiltersName`                               |
| `ReportsFiltersOperator`                           | `ReportFiltersOperator`                           |
| `ReportsFilters`                                   | `ReportFilters`                                   |
| `ReportTypeAdjustments`                            | `AdjustmentsReportType`                           |
| `FilterNameAdjustments`                            | `AdjustmentsReportFilterName`                     |
| `ReportFiltersAdjustments`                         | `AdjustmentsReportFilters`                        |
| `ReportTypeTransactions`                           | `TransactionsReportType`                          |
| `FilterNameTransactions`                           | `TransactionsReportFilterName`                    |
| `ReportFiltersTransactions`                        | `TransactionsReportFilters`                       |
| `ReportTypeProductsPrices`                         | `ProductsPricesReportType`                        |
| `FilterNameProductPrices`                          | `ProductPricesReportFilterName`                   |
| `ReportFiltersProductPrices`                       | `ProductPricesReportFilters`                      |
| `ReportTypeDiscounts`                              | `DiscountsReportType`                             |
| `FilterNameDiscounts`                              | `DiscountsReportFilterName`                       |
| `ReportFiltersDiscounts`                           | `DiscountsReportFilters`                          |
| `SubscriptionsTransactionLineItemPreview`          | `SubscriptionTransactionLineItemPreview`          |
| `SubscriptionsTaxRatesUsed`                        | `TaxRatesUsed`                                    |
| `SubscriptionsAdjustmentItem`                      | `SubscriptionsAdjustmentItem`                     |
| `SubscriptionsUpdateCatalogItem`                   | `SubscriptionUpdateCatalogItem`                   |
| `SubscriptionsCatalogItem`                         | `SubscriptionCatalogItem`                         |
| `SubscriptionsNonCatalogPriceForAnExistingProduct` | `SubscriptionNonCatalogPriceForAnExistingProduct` |
| `SubscriptionsNonCatalogPriceAndProduct`           | `SubscriptionNonCatalogPriceAndProduct`           |
| `AdjustmentsTotalsBreakdown`                       | `AdjustmentTotalsBreakdown`                       |
| `AdjustmentsTotals`                                | `TransactionAdjustmentTotals`                     |
| `TransactionsCatalogItem`                          | `TransactionCatalogItem`                          |
| `TransactionsNonCatalogPriceForAnExistingProduct`  | `TransactionNonCatalogPriceForAnExistingProduct`  |
| `TransactionsNonCatalogPriceAndProduct`            | `TransactionNonCatalogPriceAndProduct`            |
| `TransactionsTransactionsCheckout`                 | `TransactionCheckout`                             |


Some types were enumerations using constants and therefore the following constants are renamed too, below is a note of the prefix changes:

| Previous Constant Prefix   | New Constant Prefix             |
|----------------------------|---------------------------------|
| `ReportsStatus`            | `ReportStatus`                  |
| `ReportsType`              | `ReportType`                    |
| `ReportsFiltersName`       | `ReportFiltersName`             |
| `ReportsFiltersOperator`   | `ReportFiltersOperator`         |
| `ReportTypeAdjustments`    | `AdjustmentsReportType`         |
| `FilterNameAdjustments`    | `AdjustmentsReportFilterName`   |
| `ReportTypeTransactions`   | `TransactionsReportType`        |
| `FilterNameTransactions`   | `TransactionsReportFilterName`  |
| `ReportTypeProductsPrices` | `ProductsPricesReportType`      |
| `FilterNameProductPrices`  | `ProductPricesReportFilterName` |
| `ReportTypeDiscounts`      | `DiscountsReportType`           |
| `FilterNameDiscounts`      | `DiscountsReportFilterName`     |


Some types were used as part of a union and therefore the following functions are renamed too:

| Previous Function                                                                     | New Function                                                                         |
|---------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------|
| `NewCreateSubscriptionChargeItemsSubscriptionsCatalogItem`                            | `NewCreateSubscriptionChargeItemsSubscriptionCatalogItem`                            |                              
| `NewCreateSubscriptionChargeItemsSubscriptionsNonCatalogPriceForAnExistingProduct`    | `NewCreateSubscriptionChargeItemsSubscriptionNonCatalogPriceForAnExistingProduct`    |      
| `NewCreateSubscriptionChargeItemsSubscriptionsNonCatalogPriceAndProduct`              | `NewCreateSubscriptionChargeItemsSubscriptionNonCatalogPriceAndProduct`              |                
| `NewPreviewSubscriptionChargeItemsSubscriptionsCatalogItem`                           | `NewPreviewSubscriptionChargeItemsSubscriptionCatalogItem`                           |                             
| `NewPreviewSubscriptionChargeItemsSubscriptionsNonCatalogPriceForAnExistingProduct`   | `NewPreviewSubscriptionChargeItemsSubscriptionNonCatalogPriceForAnExistingProduct`   |     
| `NewPreviewSubscriptionChargeItemsSubscriptionsNonCatalogPriceAndProduct`             | `NewPreviewSubscriptionChargeItemsSubscriptionNonCatalogPriceAndProduct`             |               
| `NewTransactionPreviewByAddressItemsTransactionsCatalogItem`                          | `NewTransactionPreviewByAddressItemsTransactionCatalogItem`                          |                            
| `NewTransactionPreviewByAddressItemsTransactionsNonCatalogPriceForAnExistingProduct`  | `NewTransactionPreviewByAddressItemsTransactionNonCatalogPriceForAnExistingProduct`  |    
| `NewTransactionPreviewByAddressItemsTransactionsNonCatalogPriceAndProduct`            | `NewTransactionPreviewByAddressItemsTransactionNonCatalogPriceAndProduct`            |              
| `NewTransactionPreviewByIPItemsTransactionsCatalogItem`                               | `NewTransactionPreviewByIPItemsTransactionCatalogItem`                               |                                 
| `NewTransactionPreviewByIPItemsTransactionsNonCatalogPriceForAnExistingProduct`       | `NewTransactionPreviewByIPItemsTransactionNonCatalogPriceForAnExistingProduct`       |         
| `NewTransactionPreviewByIPItemsTransactionsNonCatalogPriceAndProduct`                 | `NewTransactionPreviewByIPItemsTransactionNonCatalogPriceAndProduct`                 |                   
| `NewTransactionPreviewByCustomerItemsTransactionsCatalogItem`                         | `NewTransactionPreviewByCustomerItemsTransactionCatalogItem`                         |                           
| `NewTransactionPreviewByCustomerItemsTransactionsNonCatalogPriceForAnExistingProduct` | `NewTransactionPreviewByCustomerItemsTransactionNonCatalogPriceForAnExistingProduct` |   
| `NewTransactionPreviewByCustomerItemsTransactionsNonCatalogPriceAndProduct`           | `NewTransactionPreviewByCustomerItemsTransactionNonCatalogPriceAndProduct`           |             


2.The consistency change of pluralised prefixes highlighted conflicting type names for `SubscriptionDiscount` and `Checkout`.

This has resulted in some type changes

| Previous Type           | New Type                            |
|-------------------------|-------------------------------------|
| `SubscriptionDiscount`  | `SubscriptionDiscountTimePeriod`    |
| `SubscriptionsDiscount` | `SubscriptionDiscountEffectiveFrom` |
| `Checkout`              | `TransactionCheckout`               |
| `TransactionsCheckout`  | `TransactionCheckout`               |


## v0.6.0

1. This update makes a significant change to the way the SDK works with the [Reports API](https://developer.paddle.com/api-reference/reports/overview)

When updating to this version if you're integrating with Reports you will need to refactor the code accordingly as it now closely matches the oneOf design of the API. 

An example generating a product/price report: 

```go
res, err := client.CreateReport(ctx,
	paddle.NewCreateReportRequestProductsAndPricesReport(&paddle.ProductsAndPricesReport{
		Type: paddle.ReportTypeProductsPricesProductsPrices,
		Filters: []paddle.ReportFiltersProductPrices{
			paddle.ReportFiltersProductPrices{
				Name:  paddle.FilterNameProductPricesPriceStatus,
				Value: []string{"archived"},
			},
			paddle.ReportFiltersProductPrices{
				Name:  paddle.FilterNameProductPricesProductStatus,
				Value: []string{"archived"},
			},
		},
	}),
)
```

## v0.5.0

1. This update fixes correctly resolves generated enum types. Prior to this enum types in our spec were using there respective basic type `string`

To upgrade to `0.5.0` from any earlier version you will need to ensure you're correctly utilising the provided constants for these types, e.g.

```go
// Instead of this
priceUpdate := &paddle.UpdatePriceRequest{Status: paddle.NewPatchField("archived")}

// paddle.Status can be passed using paddle.StatusArchived
priceUpdate := &paddle.UpdatePriceRequest{Status: paddle.NewPatchField(paddle.StatusArchived)}
```

2. Notifications and Events `EventType` property now use `EventTypeName` instead of `string` type. 

Any functions this property is passed into will need to be updated accordingly.

3. `Totals` is now a unified type from the previous 4 types `Totals`, `TotalsFormatted`, `UnitTotals` and `UnitTotalsFormatted`

The type shape is the same however receiving functions will need to be updated accordingly.

Any usage of the following types may cause breakages.

| Package              | Type                                      |
|----------------------|-------------------------------------------|
| `paddlenotification` | `TransactionLineItem`                     |
| `paddle`             | `PricePreviewLineItem`                    |
| `paddle`             | `TransactionLineItem`                     |
| `paddle`             | `SubscriptionsTransactionLineItemPreview` |
| `paddle`             | `TransactionLineItemPreview`              |

4. Various types have been renamed to be less generic and lower the risk of a future breaking change

| Previous Type                      | New Type                            |
|------------------------------------|-------------------------------------|
| `Action`                           | `AdjustmentAction`                  |
| `IPAddressesData`                  | `IPAddress`                         | 
| `Origin`                           | `NotificationOrigin`                |
| `SubscriptionsCatalogItem`         | `SubscriptionsUpdateCatalogItem`    | 
| `ResultAction`                     | `UpdateSummaryResultAction`         |
| `Result`                           | `UpdateSummaryResult`               |
| `PreviewSubscriptionUpdateSummary` | `SubscriptionPreviewUpdateSummary`  |
| `Breakdown`                        | `AdjustmentsTotalsBreakdown`        |
| `PreviewSubscriptionRequest`       | `PreviewSubscriptionUpdateRequest`  |
| `PreviewTransactionRequest`        | `PreviewTransactionCreateRequest`   |
| `CountryAndZipPostalCode`          | `TransactionPreviewByAddress`       |
| `CountryAndZipPostalCodeItems`     | `TransactionPreviewByAddressItems`  |
| `IPAddress`                        | `TransactionPreviewByIP`            |
| `IPAddressItems`                   | `TransactionPreviewByIPItems`       |
| `ExistingCustomerPaddleIDs`        | `TransactionPreviewByCustomer`      |
| `ExistingCustomerPaddleIDsItems`   | `TransactionPreviewByCustomerItems` |

Alongside these type changes some functions have been respectively updated and any usages will need to be changed in your codebase.

| Previous Function Name                                                             | New Function Name                                                                     |
|------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------|
| `NewCountryAndZipPostalCodeItemsTransactionsCatalogItem`                           | `NewTransactionPreviewByAddressItemsTransactionsCatalogItem`                          | 
| `NewCountryAndZipPostalCodeItemsTransactionsNonCatalogPriceForAnExistingProduct`   | `NewTransactionPreviewByAddressItemsTransactionsNonCatalogPriceForAnExistingProduct`  |
| `NewCountryAndZipPostalCodeItemsTransactionsNonCatalogPriceAndProduct`             | `NewTransactionPreviewByAddressItemsTransactionsNonCatalogPriceAndProduct`            |
| `NewIPAddressItemsTransactionsCatalogItem`                                         | `NewTransactionPreviewByIPItemsTransactionsCatalogItem`                               |
| `NewIPAddressItemsTransactionsNonCatalogPriceForAnExistingProduct`                 | `NewTransactionPreviewByIPItemsTransactionsNonCatalogPriceForAnExistingProduct`       |
| `NewIPAddressItemsTransactionsNonCatalogPriceAndProduct`                           | `NewTransactionPreviewByIPItemsTransactionsNonCatalogPriceAndProduct`                 |
| `NewExistingCustomerPaddleIDsItemsTransactionsCatalogItem`                         | `NewTransactionPreviewByCustomerItemsTransactionsCatalogItem`                         |
| `NewExistingCustomerPaddleIDsItemsTransactionsNonCatalogPriceForAnExistingProduct` | `NewTransactionPreviewByCustomerItemsTransactionsNonCatalogPriceForAnExistingProduct` |
| `NewExistingCustomerPaddleIDsItemsTransactionsNonCatalogPriceAndProduct`           | `NewTransactionPreviewByCustomerItemsTransactionsNonCatalogPriceAndProduct`           |
| `NewCreateSubscriptionChargeItemsSubscriptionsSubscriptionsCatalogItem`            | `NewCreateSubscriptionChargeItemsSubscriptionsCatalogItem`                            |
| `NewPreviewSubscriptionChargeItemsSubscriptionsSubscriptionsCatalogItem`           | `NewPreviewSubscriptionChargeItemsSubscriptionsCatalogItem`                           |
| `PreviewSubscription`                                                              | `PreviewSubscriptionUpdate`                                                           |
| `PreviewTransaction`                                                               | `PreviewTransactionCreate`                                                            |


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
