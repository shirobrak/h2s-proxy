# H2S Proxy

Proxy for forwarding http to socks based on rules

# Usege

0. Prepare socks proxy

1. Create proxy profile

please check `example/example-profile.json`

2. Run proxy server 
```
go run main.go --profile=${profile_path}
```
