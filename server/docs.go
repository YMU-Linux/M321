package server

import "net/http"

func docsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Payment Service API</title>
  <style>
    body { max-width: 900px; margin: 2rem auto; padding: 0 1rem; font: 16px/1.5 system-ui, sans-serif; color: #222; }
    code, pre { background: #f3f4f6; padding: .2rem .4rem; border-radius: 4px; }
    pre { overflow-x: auto; padding: 1rem; }
    h2 { margin-top: 2rem; }
    table { border-collapse: collapse; width: 100%; }
    th, td { border: 1px solid #ddd; padding: .5rem; text-align: left; }
  </style>
</head>
<body>
  <h1>Payment Service API</h1>
  <p>HTTP API for creating orders and completing payments through PayPal.</p>
  <p>Base URL: <code>http://localhost:8080</code></p>

  <h2>Payment workflow</h2>
  <ol>
    <li>Create an order.</li>
    <li>Create a payment for that order and open its <code>approval_url</code>.</li>
    <li>Approve the payment using a PayPal buyer account.</li>
    <li>Capture the payment, then retrieve the order or payment to check its status.</li>
  </ol>

  <h2>Endpoints</h2>
  <table>
    <thead><tr><th>Method</th><th>Path</th><th>Description</th></tr></thead>
    <tbody>
      <tr><td>GET</td><td><code>/sessions/current</code></td><td>Validate the caller's session with the auth service.</td></tr>
      <tr><td>POST</td><td><code>/orders</code></td><td>Create an order.</td></tr>
      <tr><td>GET</td><td><code>/orders/{id}</code></td><td>Retrieve an order.</td></tr>
      <tr><td>POST</td><td><code>/orders/{id}/pay</code></td><td>Create a PayPal order and payment.</td></tr>
      <tr><td>POST</td><td><code>/payments/{id}/capture</code></td><td>Capture an approved PayPal payment.</td></tr>
      <tr><td>GET</td><td><code>/payments/{id}</code></td><td>Retrieve a payment.</td></tr>
      <tr><td>GET</td><td><code>/docs</code></td><td>Display this documentation.</td></tr>
    </tbody>
  </table>

    <h2>Authentication and browser access</h2>
    <p>Except for <code>GET /docs</code> and CORS preflight requests, endpoints require
      a valid bearer token or session cookie. The service forwards it to the configured
      auth service's <code>GET /sessions/current</code>. A successful response is passed
      through by this endpoint; failed validation denies access. Orders and payments are
      restricted to the authenticated user who created the order.</p>
    <p>Set <code>AUTH_SERVICE_URL</code> to the auth service base URL. Set
      <code>CORS_ALLOWED_ORIGINS</code> to a comma-separated list of trusted browser
      origins to allow credentialed requests. With no value, non-credentialed CORS uses
      a wildcard origin.</p>

  <h2>Create an order</h2>
  <p><code>POST /orders</code> accepts JSON. Currency is optional and defaults to USD.
     Item subtotals and the order total are calculated by the service.</p>
  <pre>{
  "currency": "USD",
  "items": [
    {
      "product_id": "book-1",
      "name": "Example book",
      "quantity": 2,
      "unit_price": 12.50
    }
  ]
}</pre>
  <p>Success: <code>201 Created</code>, with an order containing <code>order_id</code>,
     <code>total</code>, <code>currency</code>, <code>status</code>, and items.</p>

  <h2>Create a PayPal payment</h2>
  <p>Call <code>POST /orders/{id}/pay</code>, replacing <code>{id}</code> with the
     returned order ID. A successful response includes <code>payment_id</code>,
     <code>paypal_order_id</code>, <code>approval_url</code>, and the payment status.
     Open <code>approval_url</code> and approve the payment in PayPal.</p>

  <h2>Capture an approved payment</h2>
  <p>After buyer approval, call
     <code>POST /payments/{id}/capture</code>, replacing <code>{id}</code> with
     the returned payment ID. On successful capture the payment status is updated,
     and a completed payment changes the order status to <code>PAID</code>.</p>

  <h2>Retrieve resources</h2>
  <ul>
    <li><code>GET /orders/{id}</code> returns the order.</li>
    <li><code>GET /payments/{id}</code> returns the payment, including its status
        and PayPal IDs.</li>
  </ul>

  <h2>Errors and operational notes</h2>
  <p>Errors are JSON objects with <code>error</code>, <code>message</code>,
     <code>created_at</code>, and <code>error_code</code>. Common HTTP statuses
     include <code>400</code> for invalid input, <code>404</code> for missing
     resources, and <code>502</code> when PayPal returns an error.</p>
  <p>Responses include an <code>X-Correlation-ID</code> header. Orders and
     payments are held in memory and are lost when the server restarts.
     Use PayPal sandbox credentials for testing.</p>
</body>
</html>`))
}
