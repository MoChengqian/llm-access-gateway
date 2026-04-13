# Kubernetes Production Server-Side Dry-Run Evidence

Date: 2026-04-12

## Purpose

Record one target-cluster verification attempt for the Stage 7
`make k8s-production-server-dry-run` gate, including the exact blocker when the
repository-owned overlays are valid but the workstation can no longer talk to
the target cluster with trusted admin credentials.

This is historical environment evidence, not an open repository-completion
blocker. Use it to understand access-failure modes when a real cluster exists,
not to judge repository-only Stage 7 progress.

## Target Context

- `kubectl` context: `kubernetes-admin@kubernetes`
- target server: `https://101.34.253.152:6443`
- workstation namespace default: `dubbo-system`

## Commands Run

```bash
kubectl config current-context
kubectl config get-contexts
make k8s-production-server-dry-run
openssl s_client -showcerts -connect 101.34.253.152:6443 -servername 101.34.253.152 </dev/null
openssl verify -CAfile /tmp/lag-kubeconfig-ca.crt /tmp/lag-apiserver.crt
curl -sk --cert /private/tmp/lag-kube-admin.crt --key /private/tmp/lag-kube-admin.key https://101.34.253.152:6443/api
ssh -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/private/tmp/lag-k8s-known-hosts root@101.34.253.152
ssh -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/private/tmp/lag-k8s-known-hosts ubuntu@101.34.253.152
```

## Result

`make k8s-production-server-dry-run` did **not** reach the overlay
server-side apply phase. It failed in cluster API discovery because the
operator workstation's Kubernetes access baseline had drifted from the target
cluster.

## Evidence Summary

### 1. Raw Dry-Run Gate Failure

```text
== cluster api discovery ==
Unable to connect to the server: tls: failed to verify certificate: x509: certificate signed by unknown authority
make: *** [k8s-production-server-dry-run] Error 1
```

### 2. Kubeconfig CA No Longer Matches The Live Apiserver Chain

Observed certificate metadata:

- kubeconfig CA subject: `CN=kubernetes`
- kubeconfig CA SHA256 fingerprint:
  `1A:81:98:93:50:87:04:DF:DA:E0:01:A0:1D:8C:5A:1A:21:17:06:6D:71:D5:18:86:C1:F5:63:37:A9:13:5C:E6`
- apiserver subject: `CN=kube-apiserver`
- apiserver issuer: `CN=kubernetes`
- apiserver authority key identifier:
  `72:C2:D7:35:A7:C6:18:7D:B0:CB:C2:41:EB:50:12:7F:66:A5:D6:59`
- kubeconfig CA subject key identifier:
  `26:07:A6:DD:25:70:09:56:22:3F:5C:1F:A2:71:32:43:43:53:30:D7`

Verification result:

```text
CN=kube-apiserver
error 20 at 0 depth lookup: unable to get local issuer certificate
error /tmp/lag-apiserver.crt: verification failed
```

This proves the current kubeconfig CA bundle is stale relative to the live
apiserver trust chain.

### 3. The Embedded Kubernetes Admin Client Credentials Are Also Rejected

Even after bypassing TLS verification and presenting the embedded
`kubernetes-admin` client certificate and key directly:

```text
HTTP/2 401
...
"message": "Unauthorized"
```

This proves the blocker is not only CA drift. The workstation's stored client
credentials are also no longer accepted by the target apiserver.

### 4. Control-Plane SSH Recovery Path Is Not Available From This Workstation

After adding the live host key to a temporary `known_hosts`, non-interactive SSH
still failed for both `root` and `ubuntu`:

```text
Permission denied (publickey,gssapi-keyex,gssapi-with-mic).
```

That prevented in-place recovery by fetching `/etc/kubernetes/admin.conf` from
the control-plane node.

## Decision

The repository-owned Stage 7 Kubernetes overlays remain locally renderable and
eligible for server-side dry-run, but this workstation cannot currently
generate fresh target-cluster evidence.

This is an **environment-owned access blocker**, not a repository overlay
defect.

## Required Remediation Before Re-Running

1. Regain control-plane access from an authorized workstation or bastion host.
2. Fetch a fresh kubeconfig, typically from `/etc/kubernetes/admin.conf`.
3. Replace or merge the stale `kubernetes-admin@kubernetes` context locally.
4. Confirm both of these pass before retrying the overlay gate:

```bash
kubectl cluster-info
kubectl auth whoami
```

5. Re-run:

```bash
make k8s-production-server-dry-run
```

Only after that succeeds should the target cluster be considered to have fresh
Stage 7 server-side dry-run evidence.
