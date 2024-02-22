# KOCO = Kyle's OpenVPN Certs Organizer

Simple web UI for managing certs of [kylemanna/docker-openvpn](https://github.com/kylemanna/docker-openvpn).

KOCO stands for Kyle's OpenVPN Certs Organizer, suggested by copilot.
Original idea is [**K**ylemanna/d**OC**ker-**O**penvpn](https://github.com/kylemanna/docker-openvpn)

# Build Image
```shell
docker build --build-arg https_proxy=http://host.docker.internal:1080 -t allape/koco .
```

# Run
```shell
# Initialize OpenVPN
export OVPN_DATA="$(pwd)/openvpn-config"
docker run -v $OVPN_DATA:/etc/openvpn --rm kylemanna/openvpn ovpn_genconfig -u udp://localhost:1194
docker run -v $OVPN_DATA:/etc/openvpn --rm -it kylemanna/openvpn ovpn_initpki

# Config compose.koco.yaml to change CA password or something else
# Start the daemon
docker compose -f compose.koco.yaml up -d
```
