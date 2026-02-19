# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly:

1. **Do NOT open a public issue.**
2. Email **[security@web3-frozen.dev](mailto:security@web3-frozen.dev)** or use [GitHub Private Vulnerability Reporting](https://github.com/web3-frozen/onchain-monitor/security/advisories/new).
3. Include:
   - Description of the vulnerability
   - Steps to reproduce
   - Potential impact
4. You will receive an acknowledgement within **48 hours**.
5. A fix will be released as soon as possible, and you will be credited (unless you prefer otherwise).

## Scope

- Application code in this repository
- Docker images published to `ghcr.io/web3-frozen/onchain-monitor`
- Dependencies listed in `go.mod`

## Out of Scope

- Third-party services (Binance API, Merkl API, Telegram API)
- Infrastructure / cluster configuration (separate repository)
