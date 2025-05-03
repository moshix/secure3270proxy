# secure3270proxy
This is a system to authenticate users (over TLS optionally) on 3270 terminals and then show them a list of mainframes they can connect to.

This way, every user gets to see their own list of mainframes. The applciation is completely configuration driven. It is obviously as secure as the access to your system.

For TLS, you will need to provide your own .crt and .key files from, say, let's encrypt. or generate your own. A script to generate your own set of keys is included.
  
Secure3270proxy uses the racingmars go3270 library as well as his proxy3270 stuff. Thanks, @racingmars
  
May 2025, Gubbio 
