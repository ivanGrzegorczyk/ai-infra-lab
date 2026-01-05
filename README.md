# AI Infrastructure Lab (AI-INFRA-LAB)

This repository contains the Infrastructure-as-Code (IaC) and Kubernetes manifests required to deploy a scalable environment for Artificial Intelligence workloads. The stack is hosted on Oracle Cloud Infrastructure (OCI) using ARM64 architecture.

## Core Infrastructure (Terraform)

The foundation is provisioned using Terraform to ensure environment reproducibility. The setup leverages the OCI Always Free tier to deploy an Ampere A1 Compute instance.

* **Compute Resources:**
    * **Architecture:** ARM64 Ampere (4 OCPUs).
    * **Memory:** 24 GB RAM
    * **Operating System:** Ubuntu 24.04 LTS.
* **Networking:** 
    * Virtual Cloud Network (VCN) with public subnets.
    * Security Lists configured for ingress on ports 80 (HTTP) and 443 (HTTPS).
    * DNS delegation managed via Cloudflare.

## Orchestration and Tooling

The cluster runs on K3s, a lightweight Kubernetes distribution.

* **Kubectl:** The primary command-line tool for cluster interaction. It is used to apply manifests, inspect resource states, and troubleshoot services within the `ai-lab` and `monitoring` namespaces.
* **Helm:** A package manager for Kubernetes used to deploy the `kube-prometheus-stack`. Helm simplifies the management of complex applications by grouping related resources into versioned charts.

## Security and External Access

External traffic and encryption are handled through a centralized Ingress controller and automated certificate management.

* **ClusterIssuer:** A `cert-manager` resource configured with the ACME protocol to communicate with Let's Encrypt. It automates the issuance and renewal of SSL/TLS certificates using HTTP-01 challenges.
* **Nginx Ingress Controller:** Manages external access to services. It handles TLS termination and routes traffic based on hostnames.
* **Grafana Ingress:** Configured to expose the monitoring dashboard, integrating with the global ClusterIssuer for automated HTTPS.

## Useful Commands

### Infrastructure Management (Terraform)
```bash
terraform init
terraform plan
terraform apply
terraform destroy
```

### Cluster Operations (Kubectl)
```bash
# Check overall cluster health
kubectl get all -A

# Monitor certificate issuance status
kubectl get certificate -n ai-lab

# Inspect Ingress routing and assigned addresses
kubectl describe ingress -n ai-lab
```

### Package Management (Helm)
```bash
# List all installed releases
helm list -A

# Update local chart repositories
helm repo update
```

## URLS
* **Smoke Test Application:** [https://test.zyklab.me](https://test.zyklab.me)
* **Grafana Dashboard:** [https://grafana.zyklab.me](https://grafana.zyklab.me)
