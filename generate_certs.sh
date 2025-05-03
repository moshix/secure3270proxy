#!/bin/bash
#
# Script to generate self-signed certificates for secure3270proxy
# This will create server.key and server.cert files
#

# Set default values
DAYS=3650  # 10 years validity
CN="secure3270proxy"
OUT_KEY="server.key"
OUT_CERT="server.cert"

echo "Generating TLS certificate for secure3270proxy..."
echo "================================================="
echo

# Create the private key
echo "Creating private key ($OUT_KEY)..."
openssl genrsa -out "$OUT_KEY" 2048
if [ $? -ne 0 ]; then
    echo "Error: Failed to generate RSA key."
    exit 1
fi
echo "Private key created successfully."
echo

# Create self-signed certificate
echo "Creating self-signed certificate ($OUT_CERT)..."
openssl req -new -x509 -sha256 -key "$OUT_KEY" -out "$OUT_CERT" -days $DAYS \
    -subj "/CN=$CN" \
    -addext "subjectAltName = DNS:localhost,IP:127.0.0.1"

if [ $? -ne 0 ]; then
    echo "Error: Failed to generate certificate."
    exit 1
fi
echo "Certificate created successfully."
echo

# Set permissions
chmod 600 "$OUT_KEY"
chmod 644 "$OUT_CERT"

# Show certificate info
echo "Certificate information:"
echo "========================"
openssl x509 -in "$OUT_CERT" -text -noout | grep -E 'Subject:|Issuer:|Not Before:|Not After|DNS:'

echo
echo "Done! You can now use these files for TLS in secure3270proxy."
echo "Update your secure3270.cnf file with:"
echo "  tlscert=$OUT_CERT"
echo "  tlskey=$OUT_KEY" 