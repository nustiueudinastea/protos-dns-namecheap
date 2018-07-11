#!/bin/bash

exec /root/namecheap-dns --loglevel debug --interval 20 --username $username --apiuser $api_user --token $api_token start
