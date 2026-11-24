# Google Cloud setup and deploy

This guide puts the server on Cloud Run in the `kennelfitness` project and
connects it to Claude. You run every command yourself, from the root of this
repository, with your own Google Cloud login.

Sections 1 to 6 are one-time setup. Section 7 is the deploy, which you repeat
for every new version. Section 9 adds the connector in Claude.

## What you end up with

| Thing | Name |
|---|---|
| Project | `kennelfitness` |
| Region (Cloud Run and Artifact Registry) | `us-central1` |
| Artifact Registry repository | `oura-mcp`, images at `us-central1-docker.pkg.dev/kennelfitness/oura-mcp/server:<commit>` |
| Cloud Run service | `oura-mcp`, at most one instance, public ingress |
| Runtime service account | `oura-mcp-runtime@kennelfitness.iam.gserviceaccount.com`, with no project roles |
| Secret | `connector-api-key`, which Cloud Run gives the server as `CONNECTOR_API_KEY` |
| Budget | $10 a month on the project, emailing at 50%, 90% and 100% of actual spend |

The service accepts requests from anyone on the internet, because Claude
calls it without a Google sign-in. The server itself refuses every request
without the `x-api-key` header, except `GET /health`.

The commands below set the project, region and names explicitly on each
call, so they work whatever your gcloud defaults are. `scripts/deploy.sh`
uses the same names; see section 7 to change them.

## 0. Before you start

You need:

- The [Google Cloud CLI](https://cloud.google.com/sdk/docs/install) (`gcloud`)
- Docker, running
- `openssl`, which macOS and Linux include

Log in and check that the project has billing turned on:

```sh
gcloud auth login
gcloud billing projects describe kennelfitness
```

The second command must show `billingEnabled: true`. If it does not, link a
billing account to the project in the console under **Billing**.

## 1. Turn on the APIs

```sh
gcloud services enable \
  run.googleapis.com \
  artifactregistry.googleapis.com \
  secretmanager.googleapis.com \
  billingbudgets.googleapis.com \
  --project=kennelfitness
```

`billingbudgets` is only for creating the budget from the command line in
section 6; skip it if you will use the console.

## 2. Create the image repository

```sh
gcloud artifacts repositories create oura-mcp \
  --repository-format=docker \
  --location=us-central1 \
  --description="oura-mcp server images" \
  --project=kennelfitness
```

## 3. Create the runtime service account

The service runs as its own service account instead of the default compute
account, which has broad rights over the project. This one gets no project
roles at all: its only right is reading the one secret, granted in section 4.

```sh
gcloud iam service-accounts create oura-mcp-runtime \
  --display-name="oura-mcp Cloud Run runtime" \
  --project=kennelfitness
```

## 4. Create the connector key and its secret

The connector key is 32 random bytes, written as 64 hex characters. Generate
it straight into Secret Manager, so it is never saved in a file or in your
shell history:

```sh
openssl rand -hex 32 | tr -d '\n' | gcloud secrets create connector-api-key \
  --replication-policy=automatic \
  --data-file=- \
  --project=kennelfitness
```

`tr -d '\n'` matters: the server refuses to start with a key that ends in a
newline, because no request could ever match it.

Let the runtime service account read this secret, and only this secret.
The role is granted on the secret, not on the project:

```sh
gcloud secrets add-iam-policy-binding connector-api-key \
  --member="serviceAccount:oura-mcp-runtime@kennelfitness.iam.gserviceaccount.com" \
  --role="roles/secretmanager.secretAccessor" \
  --project=kennelfitness
```

Check that the binding is on the secret:

```sh
gcloud secrets get-iam-policy connector-api-key --project=kennelfitness
```

When you need the key, for the connector in section 9 or a test in
section 8, read it back with:

```sh
gcloud secrets versions access latest --secret=connector-api-key --project=kennelfitness
```

On a Mac, add `| pbcopy` to copy it to the clipboard instead of printing it.

## 5. Let Docker push to Artifact Registry

This adds a credential helper for `us-central1-docker.pkg.dev` to your Docker
configuration:

```sh
gcloud auth configure-docker us-central1-docker.pkg.dev
```

## 6. Set the budget alert

A budget does not stop spending; it emails you. Keeping Cloud Run at one
instance with no minimum is what keeps the cost near zero.

In the console:

1. Open **Billing**, then **Budgets & alerts**, and choose **Create budget**.
2. Name it `kennelfitness monthly`. Under **Projects**, choose only
   `kennelfitness`. Keep the time range **Monthly**.
3. Set **Budget type** to **Specified amount** and the amount to **$10**.
4. Set three thresholds, each on **Actual** spend: **50%**, **90%** and
   **100%**.
5. Keep **Email alerts to billing admins and users** ticked, then
   **Finish**.

Or from the command line. First find the billing account ID, the part after
`billingAccounts/`:

```sh
gcloud billing projects describe kennelfitness --format='value(billingAccountName)'
```

Then create the budget, putting that ID in place of `BILLING_ACCOUNT_ID`.
Thresholds count actual spend unless they say otherwise, and billing admins
and users get the emails by default:

```sh
gcloud billing budgets create \
  --billing-account=BILLING_ACCOUNT_ID \
  --display-name="kennelfitness monthly" \
  --budget-amount=10USD \
  --calendar-period=month \
  --filter-projects=projects/kennelfitness \
  --threshold-rule=percent=0.5 \
  --threshold-rule=percent=0.9 \
  --threshold-rule=percent=1.0 \
  --billing-project=kennelfitness
```

## 7. Deploy

Commit or stash any changes first: the script refuses to deploy from a
working tree with uncommitted changes, so the image tag always names the
commit it was built from.

See what the deploy would do, without building or changing anything:

```sh
scripts/deploy.sh --dry-run
```

Then deploy:

```sh
scripts/deploy.sh
```

The script:

1. Checks that gcloud is logged in.
2. Builds the production image from the `Dockerfile` for `linux/amd64`
   (Cloud Run's platform, even on an Apple Silicon Mac), tagged with the
   commit.
3. Pushes it to Artifact Registry.
4. Deploys it to Cloud Run in `us-central1` with public ingress, at most one
   instance, the runtime service account, and `CONNECTOR_API_KEY` from the
   latest version of the `connector-api-key` secret.
5. Checks the deployed service: `GET /health` must answer `200`, and
   `POST /mcp` without the key must answer `401`.
6. Prints the service URL and the connector URL, which is the service URL
   followed by `/mcp`.

The deploy asks no questions, and a new version takes over from the running
one once it is ready. The names default to the ones in this guide. To
change one for a run, set it in your shell:

| Variable | Default |
|---|---|
| `GCP_PROJECT` | `kennelfitness` |
| `GCP_REGION` | `us-central1` |
| `SERVICE_NAME` | `oura-mcp` |
| `ARTIFACT_REPOSITORY` | `oura-mcp` |
| `RUNTIME_SERVICE_ACCOUNT` | `oura-mcp-runtime` |
| `API_KEY_SECRET` | `connector-api-key` |

## 8. Try it with the key

Put the service URL the deploy printed in place of `SERVICE_URL`, then call
`ping`:

```sh
curl -X POST "SERVICE_URL/mcp" \
  -H "x-api-key: $(gcloud secrets versions access latest --secret=connector-api-key --project=kennelfitness)" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ping","arguments":{}}}'
```

The answer includes `"text":"pong"`. The same request without the
`x-api-key` line gets `401 unauthorized`.

## 9. Add the connector in Claude

On Claude on the web:

1. Open **Settings**, then **Connectors**, and add a custom connector.
2. Name it `Oura` and give it the connector URL, ending in `/mcp`.
3. Choose no sign-in (no OAuth).
4. Add a request header named `x-api-key` whose value is the key from
   section 4.
5. Save it, then in a new chat turn the connector on and ask Claude to call
   the `ping` tool. It should answer `pong`.

The connector belongs to your Claude account, so it should also appear in
the Claude app on your phone. Open a new chat there, turn it on, and ask for
`ping` again. The exact labels in Claude's settings may differ slightly from
the ones above.

## Later

**Deploy a new version.** Commit, then run `scripts/deploy.sh` again.

**Read the logs.**

```sh
gcloud run services logs read oura-mcp --project=kennelfitness --region=us-central1
```

**Rotate the key.** Add a new secret version, deploy so the service reads
it, put the new key in the connector's `x-api-key` header in Claude, then
turn the old version off. Put the old version's number in place of
`OLD_VERSION`; `gcloud secrets versions list connector-api-key
--project=kennelfitness` lists them.

```sh
openssl rand -hex 32 | tr -d '\n' | gcloud secrets versions add connector-api-key \
  --data-file=- --project=kennelfitness
scripts/deploy.sh
gcloud secrets versions disable OLD_VERSION --secret=connector-api-key --project=kennelfitness
```

**Go back to an earlier version.** List the revisions, then send all
traffic to one of them. The next deploy will not take traffic back by
itself, so return to the newest with `--to-latest` when you are ready.

```sh
gcloud run revisions list --service=oura-mcp --project=kennelfitness --region=us-central1
gcloud run services update-traffic oura-mcp --to-revisions=REVISION=100 \
  --project=kennelfitness --region=us-central1
gcloud run services update-traffic oura-mcp --to-latest \
  --project=kennelfitness --region=us-central1
```

## Troubleshooting

- **The push fails with `denied` or `unauthenticated`.** Run section 5 again.
- **The deploy fails with a permission error on the service account.** Your
  login needs the Service Account User role on
  `oura-mcp-runtime`; a project Owner has it.
- **The deploy fails because the secret cannot be read.** Check the binding
  in section 4 names the runtime service account.
- **`GET /health` does not answer 200 after the deploy.** Read the logs (see
  above). A message that `CONNECTOR_API_KEY` has whitespace means the secret
  was saved with a newline; add a new version as in section 4, with
  `tr -d '\n'`, and deploy again.
- **`POST /mcp` without the key does not answer 401.** Do not use the
  service. Something other than this server's code answered; read the logs.
