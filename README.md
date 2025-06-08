<h4 align="center">Dynamically adjusts video encoding bitrate based on network conditions using the TCP-Friendly Rate Control (TFRC) algorithm</h4>

<p align="center">
  <img style="min-width:35%;" src="https://raw.githubusercontent.com/arsperger/slowcast/df891b5f2c117958592e824c60080890f7bdcee6/.github/slowcast.gif">
</p>

<p align="center">
  <a href="https://goreportcard.com/report/github.com/arsperger/slowcast"><img src="https://goreportcard.com/badge/github.com/arsperger/slowcast" alt="Go Report Card"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License: MIT"></a>
</p>
<br>

# SlowCast: Adaptive Video Streaming with TFRC

Go (golang) implementation of TCP-Friendly Rate Control for smooth and adaptive video streaming over unreliable networks.

SlowCast dynamically adjusts video encoding bitrate based on network conditions using the TCP-Friendly Rate Control (TFRC) algorithm.

## Installation on Linux (x86_64)

### Prerequisites

Install required libraries:

```bash
sudo apt update
sudp apt-get update && apt-get install -y \
    gstreamer1.0-tools \
    gstreamer1.0-plugins-base \
    gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-bad \
    gstreamer1.0-plugins-ugly \
    gstreamer1.0-libav \
    gstreamer1.0-x

# Clone the repository
git clone https://github.com/arsperger/slowcast.git
cd slowcast

# Build the application
go build
```

## Usage

```sh
# Basic usage with default settings (localhost streaming)
./slowcast

# Stream to a remote host
UDP_SINK_HOST=192.168.1.100 UDP_SINK_PORT=6000 ./slowcast

# Configure source address and port
UDP_SRC_HOST=192.168.1.5 UDP_SRC_PORT=6010 ./slowcast
```

## Configuration

SlowCast uses environment variables for configuration:

| Variable     | Description                    | Default  |
|--------------|--------------------------------|----------|
|UDP_SINK_HOST | Destination IP for RTP stream  | 127.0.0.1|
|UDP_SINK_PORT | Destination port for RTP stream|      6000|
|UDP_SRC_HOST  | Source IP for binding          | 127.0.0.1|
|UDP_SRC_PORT  | Source port for binding        |      7000|

Additional parameters (currently hardcoded):

- Min bitrate: 500 Kbps
- Max bitrate: 4000 Kbps
- Initial bitrate: 500 Kbps
- Bitrate update interval: 500ms

## Known Issues and Limitations

- Fixed packet size assumption may reduce accuracy for variable size packets
- No dynamic resolution/framerate adjustment based on bitrate changes
- new to come

## Future Improvements

- [ ] Dynamic resolution adaptation based on available bandwidth
- [ ] Better zero-loss handling algorithm
- [ ] Bandwidth probing for faster convergence
- [ ] Enhanced congestion detection beyond packet loss
- [ ] Monitoring and dynamic configuration

TBD

## Contributors

@arsperger

📢 Note: This implementation follows RFC 5348 for the TFRC algorithm and RFC 8083 for RTP/RTCP extensions
