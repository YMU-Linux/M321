# Development and PayPal Sandbox Guide

## Setup

1. Create a PayPal developer app and obtain its **Sandbox** client ID and secret.
2. Copy `.env.example` to `.env` and fill in the sandbox credentials.
3. In PowerShell, load the values before running the app:

   ```powershell
   Get-Content .env | ForEach-Object {
       if ($_ -match '^\s*([^#][^=]*)=(.*)$') {
           [Environment]::SetEnvironmentVariable($matches[1].Trim(), $matches[2].Trim(), 'Process')
       }
   }
   go run .
   ```

The server uses sandbox unless `PAYPAL_ENV=live` is explicitly set. Do not use live credentials for development.

## Payment flow

1. `POST /orders` with `items` and optional `currency` (defaults to USD).
2. `POST /orders/{order_id}/pay`; open the returned `approval_url` and approve using a sandbox buyer.
3. `POST /payments/{payment_id}/capture` after approval.
4. `GET /orders/{order_id}` or `GET /payments/{payment_id}` to inspect status.

All order and payment records are in memory and are lost when the server stops. This is suitable for local testing, not production.

## Checks

Run `gofmt` on changed Go files, then `go test ./...` and `go vet ./...`.