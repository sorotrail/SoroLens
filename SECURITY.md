# Security policy

SoroLens is a read-only explorer over public chain data. Its attack
surface is correspondingly small, but not zero: it talks to a Postgres
database and a Stellar RPC endpoint (or a SoroTrail indexer), and serves
an unauthenticated HTTP API.

## Reporting a vulnerability

Email security concerns to the repository owner via the GitHub
"Contact" link on the profile (sorotrail). Please do not open a public
issue for anything you believe is exploitable.

Include reproduction steps and, where relevant, the request that
triggers it. A proof of concept is welcome but not required.

## Scope

In scope:

- The HTTP API and web UI, including anything reachable through
  unauthenticated requests
- The ingest path against a malicious or misbehaving RPC endpoint
- The upstream path against a malicious SoroTrail instance

Out of scope:

- Deployments that expose the API to untrusted networks without their
  own access control — the README is explicit that the API is
  unauthenticated by design
- Vulnerabilities in dependencies, unless triggerable through SoroLens
  in a way a patch here would fix

## Hardening notes for operators

- Run SoroLens on a trusted network or behind a reverse proxy with auth.
- The API never mutates state; the worst a request can do is read
  chain-public data or waste resources. Rate-limit at the proxy.
