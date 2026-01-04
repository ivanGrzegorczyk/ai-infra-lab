#!/bin/bash
# Abrir puertos en el firewall interno de la imagen de Oracle
iptables -I INPUT 6 -p tcp --dport 80 -j ACCEPT
iptables -I INPUT 6 -p tcp --dport 443 -j ACCEPT
iptables -I INPUT 6 -p tcp --dport 6443 -j ACCEPT
netfilter-persistent save

# Instalar Docker
curl -fsSL https://get.docker.com | sh
usermod -aG docker ubuntu
