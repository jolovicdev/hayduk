# Security policy

Hayduk is a graphical attack management console for Metasploit, for use
against systems you are authorized to test. This file states the trust
model honestly so operators can decide where the tool may run.

## Threat model

### The link between browser and hayduk

Every hayduk instance is served behind a single bearer token: the one-time
URL printed at startup (`http://host:port/?token=...`). Anyone who obtains
that link gains full control of the connected msfrpcd session: launching
exploits, reading credentials and loot, and driving sessions.

- The token is transmitted and stored in a plain HTTP cookie; there is no
  TLS. Anyone who can read the network path between browser and hayduk
  can capture it.
- The URL (with token) appears in shell history and process listings of
  the machine that launched hayduk.

### Team mode

Team mode (`--team`) exists for several operators on one shared campaign,
and it inherits the same single-token design:

- The whole team shares one bearer link. There is no per-operator
  authentication and no revocation of individual operators.
- Operator labels are unauthenticated: any connected browser may claim
  any name. Attribution in the event log is an aid to collaboration,
  not an identity system.
- All traffic is plain HTTP, including session output and credentials
  shown in the UI.

Run team mode only on networks you trust, treat the token link exactly
like a password, and prefer single-operator mode on a loopback bind
whenever a shared campaign is not required.

### The lab in scripts/msf

The disposable docker lab binds msfrpcd to 127.0.0.1 only, with a
documented throwaway password. The vulnbox and sshbox containers publish
no ports and are reachable only inside the lab network. Never point the
lab's credentials or containers at production systems.

## Reporting a vulnerability

Open a private security advisory on GitHub (Report a vulnerability) or
contact the maintainer at https://github.com/jolovicdev. Please do not
open public issues for vulnerabilities.
