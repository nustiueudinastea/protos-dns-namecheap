#!/bin/bash

exec /go/src/namecheap-dns/namecheap-dns --loglevel debug --interval 180 --username $username --apiuser $api_user --token $api_token --domain $domain start
