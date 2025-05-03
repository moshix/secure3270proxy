# secure3270proxy
This is a system to authenticate users (over TLS optionally) on 3270 terminals and then show them a list of mainframes they can connect to.

This way, every user gets to see their own list of mainframes. The applciation is completely configuration driven. It is obviously as secure as your host system... but not more. 

For TLS, you will need to provide your own .crt and .key files from, say, let's encrypt. or generate your own. A script to generate your own set of keys is included.
  
Secure3270proxy uses the racingmars go3270 library as well as his proxy3270 stuff. Thanks, @racingmars

Usage
=====

Edit secure3270.cnf configuration file and adapt it to your needs.

Edit the users.cnf file and adapt it to your needs. 

go mod tidy

go build

./secure3270proxy
  
May 2025, Gubbio 
