# Paddle GO SDK

[Paddle Billing](https://www.paddle.com/billing?utm_source=dx&utm_medium=paddle-go-sdk) is a complete digital product sales and subscription management platform, designed for modern software businesses. It helps you increase your revenue, retain customers, and scale your operations.

This is a [Go](https://go.dev/) SDK that you can use to integrate Paddle Billing with applications written in server-side Go.

For working with Paddle in your frontend, use [Paddle.js](https://developer.paddle.com/paddlejs/overview?utm_source=dx&utm_medium=paddle-go-sdk). You can open checkouts, securely collect payment information, build pricing pages, and integrate with Paddle Retain.  

> **Important:** This package works with Paddle Billing. It does not support Paddle Classic. To work with Paddle Classic, see: [Paddle Classic API reference](https://developer.paddle.com/classic/api-reference/1384a288aca7a-api-reference?utm_source=dx&utm_medium=paddle-go-sdk)

## Learn more

- [Paddle API reference](https://developer.paddle.com/api-reference/overview?utm_source=dx&utm_medium=paddle-go-sdk)
- [Sign up for Paddle Billing](https://login.paddle.com/signup?utm_source=dx&utm_medium=paddle-go-sdk)

## Requirements

Go 1.21 or later

## Before you begin

If you've used this SDK, we'd love to hear how you found it! Email us at [team-dx@paddle.com](mailto:team-dx@paddle.com) with any thoughts.

While in early access, we may introduce breaking changes. Where we can, we'll tag breaking changes and communicate ahead of time.

## Installation

Make sure your project is using Go Modules (it will have a go.mod file in its root if it already is):

```bash
go mod init
```

To install the Paddle Go SDK, use the following command:

```bash
go get github.com/PaddleHQ/paddle-go-sdk
```

Then, reference paddle-go-sdk in a Go program with import:

```go
import (
    paddle "github.com/PaddleHQ/paddle-go-sdk"
)
```

## Usage

To authenticate, you'll need an API key. You can create and manage API keys in **Paddle > Developer tools > Authentication**.

Pass your API key while initializing a new Paddle client.

``` go
import (
    paddle "github.com/PaddleHQ/paddle-go-sdk"
)

client, err := paddle.New(
    os.Getenv("PADDLE_API_KEY"),
    paddle.WithBaseURL(paddle.ProductionBaseURL),
)
```

You can now use the client to make requests to the Paddle API.

## Examples

### List all entities

You can list supported entities with the `List*` function. It returns an iterator to help when working with multiple pages.
``` go
products, err := client.ListProducts(ctx, &paddle.ListProductsRequest{IncludePrices: true})
if err != nil {
    panic(err)
}

err = products.Iter(ctx, func(p *paddle.ProductWithIncludes) (bool, error) {
    // Do something with the product
    fmt.Printf("%+v\n", p)
    return true, nil
})
if err != nil {
    panic(err)
}
```

### Create an entity

You can create a supported entity with the `Create*` function. It accepts the resource's corresponding `Create*Request` operation e.g. `CreateProductRequest`. The created entity is returned.

``` go
product, err := client.CreateProduct(ctx, &paddle.CreateProductRequest{
    Name:        "Test Product - GO SDK",
    TaxCategory: paddle.TaxCategoryStandard,
})
if err != nil {
    panic(err)
}
fmt.Printf("%+v\n", product)
```

### Update an entity

You can update a supported entity with the `Update*` function. It accepts the `ID` of the entity to update and the corresponding `Update*Request` operation e.g. `UpdateProductRequest`. The updated entity is returned.

``` go
product, err := client.UpdateProduct(ctx, &paddle.UpdateProductRequest{
    ProductID: product.ID,
    Name:      paddle.NewPatchField("Test Product - GO SDK Updated"),
})
if err != nil {
    panic(err)
}
fmt.Printf("%+v\n", product)
```

### Get an entity

You can get an entity with the `Get*` function. It accepts the `ID` of the entity to get and the corresponding `Get*Request` operation e.g. `GetProductRequest`. The entity is returned.

``` go
product, err := client.GetProduct(ctx, &paddle.GetProductRequest{
    ProductID: productID, 
    IncludePrices: true
})
if err != nil {
    panic(err)
}
fmt.Printf("%+v\n", product)
```

## Resources

### Webhook signature verification

The SDK includes a couple of helper functions to verify webhook signatures sent by Notifications from Paddle.

You could use a middleware to verify the signature of the incoming request before processing it.

```go
verifier := paddle.NewWebhookVerifier(os.Getenv("WEBHOOK_SECRET_KEY"))
// Wrap your handler with the verifier.Middleware method
handler := verifier.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    // The request making it this far means the webhook was verified
    // Best practice here is to check if you have processed this webhook already using the event id
    // At this point you should store for async processing
    // For example a local queue or db entry

    // Respond as soon as possible with a 200 OK
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"success": true}`))
}))
```

Alternatively you can verify the signature of the incoming request manually.

``` go
webhookVerifier := paddle.NewWebhookVerifier(os.Getenv("WEBHOOK_SECRET_KEY"))
// Note: the request (req *http.Request) should be pass exactly as it comes without altering it.
ok, err := webhookVerifier.Verify(req)
```

## Feature parity

While in early access, not all operations in the Paddle API are available in our Go SDK. We're working on complete feature parity for our final release.

This table shows which operations are available as of the latest release.

| Paddle API operation                         | Support  |
|----------------------------------------------|:--------:|
| **Products**                                 | **Full** |
| List products                                |    ✅     |
| Create a product                             |    ✅     |
| Get a product                                |    ✅     |
| Update a product                             |    ✅     |
| **Prices**                                   | **Full** |
| List prices                                  |    ✅     |
| Create a price                               |    ✅     |
| Get a price                                  |    ✅     |
| Update a price                               |    ✅     |
| **Discounts**                                | **Full** |
| List discounts                               |    ✅     |
| Create a discount                            |    ✅     |
| Get a discount                               |    ✅     |
| Update a discount                            |    ✅     |
| **Customers**                                | **Full** |
| List customers                               |    ✅     |
| Create a customer                            |    ✅     |
| Get a customer                               |    ✅     |
| Update a customer                            |    ✅     |
| List credit balances for a customer          |    ✅     |
| **Addresses**                                | **Full** |
| List addresses for a customer                |    ✅     |
| Create an addresses for a customer           |    ✅     |
| Get an address for a customer                |    ✅     |
| Update an address for a customer             |    ✅     |
| **Businesses**                               |    ✅     |
| List businesses for a customer               |    ✅     |
| Create a business for a customer             |    ✅     |
| Get a business for a customer                |    ✅     |
| Update a business for a customer             |    ✅     |
| **Transactions**                             | **Full** |
| List transactions                            |    ✅     |
| Create a transaction                         |    ✅     |
| Get a transaction                            |    ✅     |
| Update a transaction                         |    ✅     |
| Preview a transaction                        |    ✅     |
| Get a PDF invoice for a transaction          |    ✅     |
| **Subscriptions**                            | **Full** |
| List subscriptions                           |    ✅     |
| Get a subscription                           |    ✅     |
| Preview an update to a subscription          |    ✅     |
| Update a subscription                        |    ✅     |
| Get a transaction to update payment method   |    ✅     |
| Preview a one-time charge for a subscription |    ✅     |
| Create a one-time charge for a subscription  |    ✅     |
| Activate a trialing subscription             |    ✅     |
| Pause a subscription                         |    ✅     |
| Resume a paused subscription                 |    ✅     |
| Cancel a subscription                        |    ✅     |
| **Adjustments**                              | **Full** |
| List adjustments                             |    ✅     |
| Create an adjustment                         |    ✅     |
| **Pricing preview**                          | **Full** |
| Preview prices                               |    ✅     |
| **Reports**                                  | **Full** |
| List reports                                 |    ✅     |
| Create a report                              |    ✅     |
| Get a report                                 |    ✅     |
| Get a CSV file for a report                   |    ✅     |
| **Notification settings**                    | **Full** |
| List notification settings                   |    ✅     |
| Create a notification setting                |    ✅     |
| Get a notification setting                   |    ✅     |
| Update a notification setting                |    ✅     |
| Delete a notification setting                |    ✅     |
| **Event types**                              | **Full** |
| List event types                             |    ✅     |
| **Events**                                   | **Full** |
| List events                                  |    ✅     |
| **Notifications**                            | **Full** |
| List notifications                           |    ✅     |
| Get a notification                           |    ✅     |
| Replay a notification                        |    ✅     |
| **Notification logs**                        | **Full** |
| List logs for a notification                 |    ✅     |

## Learn more

- [Paddle API reference](https://developer.paddle.com/api-reference/overview?utm_source=dx&utm_medium=paddle-go-sdk)
- [Sign up for Paddle Billing](https://login.paddle.com/signup?utm_source=dx&utm_medium=paddle-go-sdk)
