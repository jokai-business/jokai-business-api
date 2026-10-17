Open this collection in Bruno. Set `baseUrl=https://localhost:8443`, trust the generated local certificate, and provide a short-lived `accessToken` as a secret variable for the authenticated request. Never save a token in the collection. Browser/API E2E tests obtain tokens through real PKCE login and cover the authenticated matrix without copying tokens.

Delivery examples require a project manager token (administrator or delivery user). Choose a won `opportunityId` from project sources for Create delivery, then set `projectId` from its response for task requests. Accepted-quote handoffs also supply `quoteId`. Each opportunity supports one project.

Finance examples require administrator/finance permissions, an accepted quoteId, and invoiceId from creation. Use a fresh UUID paymentKey for each logical payment; reuse it only for an identical retry. Adjust sample dates/amounts for the test invoice. Issue locks draft amounts. These requests record state and receipts; they do not send messages or charge accounts.

Scope examples start with a Sales token and opportunityId. Set scopeId from creation, use the current version for each write, and switch to a different manager/admin who did not prepare or submit the revision for approval. Approved revision reads are immutable snapshots. Scope roles do not grant access to external drive files.

Settings examples require an organization administrator token. Read `/api/v1/settings/organization` before a write and supply its current version; profile saves and logo changes increment the same version. Logo upload uses multipart file/version; removal uses JSON version. Settings cannot select a foreign organization. SMTP/integration administration is outside the current MVP.

Email administration examples are read-only. Test-send and retry POST endpoints require explicit confirmation; test requests also require an idempotency UUID and always target the current administrator's membership email. These actions are unavailable while SMTP is disabled. Do not enable SMTP merely to run the test collection; the automated integration suite uses a fake sender.
