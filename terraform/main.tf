# Buscador automático de la imagen de Ubuntu 24.04 ARM
data "oci_core_images" "ubuntu_arm" {
  compartment_id           = var.compartment_id
  operating_system         = "Canonical Ubuntu"
  operating_system_version = "24.04"
  shape                    = "VM.Standard.A1.Flex" # Esto filtra solo las compatibles con ARM
  sort_by                  = "TIMECREATED"
  sort_order               = "DESC"
}

# Obtener el Availability Domain (necesario para la VM)
data "oci_identity_availability_domains" "ads" {
  compartment_id = var.compartment_id
}

# Crear la Red Virtual (VCN)
resource "oci_core_vcn" "ai_lab_vcn" {
  cidr_block     = "10.0.0.0/16"
  compartment_id = var.compartment_id
  display_name   = "ai-lab-vcn"
  dns_label      = "ailab"
}

# Crear el Internet Gateway (Salida a Internet)
resource "oci_core_internet_gateway" "ai_lab_ig" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.ai_lab_vcn.id
  display_name   = "ai-lab-gateway"
}

# Tabla de ruteo
resource "oci_core_default_route_table" "ai_lab_rt" {
  manage_default_resource_id = oci_core_vcn.ai_lab_vcn.default_route_table_id
  route_rules {
    network_entity_id = oci_core_internet_gateway.ai_lab_ig.id
    destination       = "0.0.0.0/0"
  }
}

# Security List (Firewall de Oracle)
resource "oci_core_security_list" "ai_lab_sl" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.ai_lab_vcn.id
  display_name   = "ai-lab-security-list"

  egress_security_rules {
    protocol    = "all"
    destination = "0.0.0.0/0"
  }

  # Ingress: SSH, HTTP, HTTPS y K3s
  dynamic "ingress_security_rules" {
    for_each = [22, 80, 443, 6443]
    content {
      protocol = "6" # TCP
      source   = "0.0.0.0/0"
      tcp_options {
        min = ingress_security_rules.value
        max = ingress_security_rules.value
      }
    }
  }
}

# Subred
resource "oci_core_subnet" "ai_lab_subnet" {
  cidr_block        = "10.0.1.0/24"
  display_name      = "ai-lab-subnet"
  compartment_id    = var.compartment_id
  vcn_id            = oci_core_vcn.ai_lab_vcn.id
  security_list_ids = [oci_core_security_list.ai_lab_sl.id]
  dns_label         = "aisubnet"
}

# La Instancia (VM)
resource "oci_core_instance" "ai_server" {
  availability_domain = data.oci_identity_availability_domains.ads.availability_domains[0].name
  compartment_id      = var.compartment_id
  shape               = "VM.Standard.A1.Flex"
  display_name        = "ai-infra-node"

  shape_config {
    memory_in_gbs = 24
    ocpus         = 4
  }

  create_vnic_details {
    subnet_id        = oci_core_subnet.ai_lab_subnet.id
    assign_public_ip = true
  }

  source_details {
    source_type             = "image"
    source_id               = data.oci_core_images.ubuntu_arm.images[0].id # Usar la imagen encontrada
    boot_volume_size_in_gbs = 200
  }

  metadata = {
    ssh_authorized_keys = var.ssh_public_key
    user_data           = base64encode(file("cloud-init.sh"))
  }
}
