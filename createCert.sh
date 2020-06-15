#!/bin/bash
#create rsa private key by 720 days expire
openssl genrsa -out private_$(date +%y_%m_%d).key 2048
#Create a self-signed cerificate
openssl req -x509 -sha256 -nodes -new -days 365 -key private_$(date +%y_%m_%d).key -out cert.pem