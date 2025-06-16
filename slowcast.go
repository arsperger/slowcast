package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-gst/go-glib/glib"
	"github.com/go-gst/go-gst/gst"
	"github.com/pion/rtcp"

	"github.com/arsperger/slowcast/pkg/tfrc"
)

const (
	appName    = "SlowCast"
	appVersion = "0.1.0"
	appDesc    = "Adaptive bitrate video streaming with TFRC"
)

type SlowCast struct {
	minBitrate     int
	maxBitrate     int
	currentBitrate int
	lastChange     time.Time
	changeInterval time.Duration
	stream         *gst.Pipeline
	mainLoop       *glib.MainLoop
	debugEnabled   bool
}

// TODO: configurable
func (s *SlowCast) MakeSlowCast(debug bool) *SlowCast {
	return &SlowCast{
		minBitrate:     500, // Kbps
		maxBitrate:     4000,
		currentBitrate: 500,
		lastChange:     time.Now(),
		changeInterval: 500 * time.Millisecond,
		stream:         nil,
		mainLoop:       nil,
		debugEnabled:   debug,
	}
}

func (s *SlowCast) dumpPipelineDot() {
	if s.debugEnabled && s.stream != nil {
		dotFile := "pipeline.dot"
		fmt.Printf("Dumping pipeline structure to %s\n", dotFile)

		if os.Getenv("GST_DEBUG_DUMP_DOT_DIR") == "" {
			cwd, _ := os.Getwd()
			if err := os.Setenv("GST_DEBUG_DUMP_DOT_DIR", cwd); err != nil {
				fmt.Fprintf(os.Stderr, "Error setting GST_DEBUG_DUMP_DOT_DIR: %v\n", err)
			}
		}

		s.stream.DebugBinToDotFile(gst.DebugGraphShowAll, "slowcast-pipeline")
		fmt.Println("Pipeline DOT file created successfully")
	}
}

func (s *SlowCast) setupRTCPListener(srcHost string, srcPort int) {
	rtcpPort := srcPort + 1
	addr := net.UDPAddr{IP: net.ParseIP(srcHost), Port: rtcpPort}
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		fmt.Printf("RTCP listener error: %v\n", err)
		return
	}
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing RTCP connection: %v\n", err)
		}
	}()

	// Initialize TFRC with initial bitrate and limits
	controller := tfrc.New(s.currentBitrate, s.minBitrate, s.maxBitrate)

	timeStarted := time.Now()

	buf := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading RTCP: %v\n", err)
			continue
		}
		pkts, err := rtcp.Unmarshal(buf[:n])
		if err != nil {
			fmt.Fprintf(os.Stderr, "RTCP Unmarshal error: %v\n", err)
			continue
		}

		now := time.Now()

		for _, pkt := range pkts {
			switch rr := pkt.(type) {
			case *rtcp.ReceiverReport:
				for _, report := range rr.Reports {

					elapsed := time.Since(timeStarted).Seconds()
					// Get RTCP report data
					controller.PreProcessRTCP(now, report.LastSenderReport, report.Delay, report.FractionLost)

					// fmt.Printf("{RTCP [%d] RR: elapsed=%.3f loss=%.6f, rtt=%.4fs, smoothed RTT=%.4fs, jitter=%d\n",
					//	report.SSRC, elapsed, controller.GetLastFraction(), controller.GetRttSample(), controller.GetSmoothedRTT(), report.Jitter)

					fmt.Printf("{\"SSRC\": %d, \"elapsed\": %.3f, \"loss\": %.6f, \"rtt\": %.4f, \"smoothed_rtt\": %.4f, \"jitter\": %d, \"type\": \"RTCP_RR\"}\n",
						report.SSRC, elapsed, controller.GetLastFraction(), controller.GetRttSample(), controller.GetSmoothedRTT(), report.Jitter)

					// Pace updates
					if time.Since(s.lastChange) < s.changeInterval {
						fmt.Printf("Pacing updates: %v\n", time.Since(s.lastChange))
						continue
					}

					newBr := controller.ComputeTFRCBitrate()
					if newBr != s.currentBitrate {
						s.setNewBitrate(newBr)
						s.lastChange = now
						fmt.Printf("Updated bitrate %d -> %d Kbps\n", s.currentBitrate, newBr)
						// TODO: change resolution and framerate based on new bitrate
					}
				}
			default:
				// ignore
			}
		}
	}
}

//nolint:gosec
func (s *SlowCast) setNewBitrate(kbps int) {
	enc, err := s.stream.GetElementByName("encoder")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Encoder element lookup error: %v\n", err)
		return
	}
	if err := enc.Set("bitrate", uint(kbps)); err != nil {
		fmt.Fprintf(os.Stderr, "Error setting encoder bitrate: %v\n", err)
		return
	}
	s.currentBitrate = kbps
}

// FIXME: need refactoring
//
//nolint:cyclop,gocyclo,funlen
func (s *SlowCast) createPipeline(sinkHost, srcHost string, sinkPort, srcPort int) error {

	gst.Init(nil)

	sinkRtcpPort := sinkPort + 1
	srcRtcpPort := srcPort + 2 // send from +2, bc of the RTCP listener is on srcPort + 1

	// Create pipeline
	pipeline, err := gst.NewPipeline("video-pipeline")
	if err != nil {
		return fmt.Errorf("failed to create pipeline: %w", err)
	}

	// Video source and encoding elements
	src, err := gst.NewElementWithProperties("v4l2src", map[string]interface{}{"device": "/dev/video0"})
	if err != nil {
		return fmt.Errorf("failed to create v4l2src: %w", err)
	}

	capsIn := gst.NewCapsFromString("video/x-raw,width=640,height=480,framerate=30/1")
	capsFilterIn, err := gst.NewElementWithProperties("capsfilter", map[string]interface{}{"caps": capsIn})
	if err != nil {
		return fmt.Errorf("failed to create capsfilter in: %w", err)
	}

	convert, err := gst.NewElement("videoconvert")
	if err != nil {
		return fmt.Errorf("failed to create videoconvert: %w", err)
	}

	capsOut := gst.NewCapsFromString("video/x-raw,format=I420,width=640,height=480")
	capsFilterOut, err := gst.NewElementWithProperties("capsfilter", map[string]interface{}{"caps": capsOut})
	if err != nil {
		return fmt.Errorf("failed to create capsfilter out: %w", err)
	}

	// Video encoder x264enc software encoder
	encoder, err := gst.NewElementWithProperties("x264enc", map[string]interface{}{
		"name":    "encoder",
		"bitrate": uint(s.currentBitrate), //nolint:gosec
		"tune":    "zerolatency",
	})
	if err != nil {
		return fmt.Errorf("failed to create x264enc: %w", err)
	}

	// RTP payloading element
	pay, err := gst.NewElementWithProperties("rtph264pay", map[string]interface{}{
		"pt":              uint(96),
		"config-interval": 1,
	})
	if err != nil {
		return fmt.Errorf("failed to create rtph264pay: %w", err)
	}

	// RTP caps to explicitly set media type and payload
	rtpCaps := gst.NewCapsFromString("application/x-rtp,media=video,encoding-name=H264,payload=96")
	rtpCapsFilter, err := gst.NewElementWithProperties("capsfilter", map[string]interface{}{"caps": rtpCaps})
	if err != nil {
		return fmt.Errorf("failed to create RTP capsfilter: %w", err)
	}

	// Create rtpsession element for explicit RTCP control
	rtpSession, err := gst.NewElement("rtpsession")
	if err != nil {
		return fmt.Errorf("failed to create rtpsession: %w", err)
	}

	// Configure rtpsession for RTCP SR sending
	if err = rtpSession.Set("rtp-profile", uint64(2)); err != nil { // 1 - AVP, 2 = AVPF
		fmt.Println("Warning: failed to set rtp-profile, using default")
	}
	if err = rtpSession.Set("rtcp-min-interval", uint64(5000000000)); err != nil { // 5 seconds in us
		return fmt.Errorf("failed to set rtcp-min-interval: %w", err)
	}
	if err = rtpSession.Set("rtcp-fraction", 0.05); err != nil {
		return fmt.Errorf("failed to set rtcp-fraction: %w", err)
	}
	if err = rtpSession.Set("bandwidth", uint64(0)); err != nil {
		fmt.Println("Warning: failed to set bandwidth, using default")
	}
	if err = rtpSession.Set("rtcp-sync-send-time", true); err != nil {
		return fmt.Errorf("failed to set rtcp-sync-send-time: %w", err)
	}

	// Create RTP funnel for combining payloaded streams
	rtpFunnel, err := gst.NewElement("funnel")
	if err != nil {
		return fmt.Errorf("failed to create RTP funnel: %w", err)
	}

	// Create UDP sinks for RTP
	rtpSink, err := gst.NewElementWithProperties("udpsink", map[string]interface{}{
		"host":         sinkHost,
		"port":         sinkPort,
		"bind-address": srcHost,
		"bind-port":    srcPort,
		"sync":         false,
		"async":        false,
	})
	if err != nil {
		return fmt.Errorf("failed to create rtp udpsink: %w", err)
	}

	// Create UDP sink for RTCP
	rtcpSink, err := gst.NewElementWithProperties("udpsink", map[string]interface{}{
		"host":         sinkHost,
		"port":         sinkRtcpPort,
		"bind-address": srcHost,
		"bind-port":    srcRtcpPort,
		"sync":         false,
		"async":        false,
	})
	if err != nil {
		return fmt.Errorf("failed to create rtcp udpsink: %w", err)
	}

	// Create UDP source for RTCP sender reports
	rtcpSrc, err := gst.NewElementWithProperties("udpsrc", map[string]interface{}{
		"address": srcHost,
		"port":    srcRtcpPort,
	})
	if err != nil {
		return fmt.Errorf("failed to create rtcp udpsrc: %w", err)
	}

	// Add all elements to pipeline
	if err = pipeline.AddMany(src, capsFilterIn, convert, capsFilterOut,
		encoder, pay, rtpCapsFilter, rtpSession, rtpFunnel,
		rtpSink, rtcpSink, rtcpSrc); err != nil {
		return fmt.Errorf("failed to add elements to pipeline: %w", err)
	}

	// Link video capture elements
	if err = gst.ElementLinkMany(src, capsFilterIn, convert, capsFilterOut, encoder, pay, rtpCapsFilter); err != nil {
		return fmt.Errorf("failed to link video elements: %w", err)
	}

	// Link RTP capsfilter to rtpsession send_rtp_sink
	rtpCapsFilterSrcPad := rtpCapsFilter.GetStaticPad("src")
	if rtpCapsFilterSrcPad == nil {
		return fmt.Errorf("failed to get RTP capsfilter src pad")
	}

	rtpSessionSinkPad := rtpSession.GetRequestPad("send_rtp_sink")
	if rtpSessionSinkPad == nil {
		return fmt.Errorf("failed to get rtpsession send_rtp_sink pad")
	}

	if rtpCapsFilterSrcPad.Link(rtpSessionSinkPad) != gst.PadLinkOK {
		return fmt.Errorf("failed to link RTP capsfilter to rtpsession")
	}
	// End of RTP capsfilter linking

	// Link rtpsession send_rtp_src to RTP sink
	rtpSessionSrcPad := rtpSession.GetStaticPad("send_rtp_src")
	if rtpSessionSrcPad == nil {
		return fmt.Errorf("failed to get rtpsession send_rtp_src pad")
	}

	rtpSinkPad := rtpSink.GetStaticPad("sink")
	if rtpSinkPad == nil {
		return fmt.Errorf("failed to get RTP sink pad")
	}

	if rtpSessionSrcPad.Link(rtpSinkPad) != gst.PadLinkOK {
		return fmt.Errorf("failed to link rtpsession to RTP sink")
	}
	// End of RTP sink linking

	// Link rtpsession send_rtcp_src to RTCP sink
	rtcpSrcPad := rtpSession.GetRequestPad("send_rtcp_src")
	if rtcpSrcPad == nil {
		return fmt.Errorf("failed to get rtpsession send_rtcp_src pad")
	}

	rtcpSinkPad := rtcpSink.GetStaticPad("sink")
	if rtcpSinkPad == nil {
		return fmt.Errorf("failed to get RTCP sink pad")
	}

	if rtcpSrcPad.Link(rtcpSinkPad) != gst.PadLinkOK {
		return fmt.Errorf("failed to link rtpsession to RTCP sink")
	}
	// End of RTCP sink linking

	// Link RTCP source to rtpsession recv_rtcp_sink
	rtcpSrcSrcPad := rtcpSrc.GetStaticPad("src")
	if rtcpSrcSrcPad == nil {
		return fmt.Errorf("failed to get RTCP source src pad")
	}

	rtcpSinkPad = rtpSession.GetRequestPad("recv_rtcp_sink")
	if rtcpSinkPad == nil {
		return fmt.Errorf("failed to get rtpsession recv_rtcp_sink pad")
	}

	if rtcpSrcSrcPad.Link(rtcpSinkPad) != gst.PadLinkOK {
		return fmt.Errorf("failed to link RTCP source to rtpsession")
	}
	// End of RTCP source linking

	fmt.Println("Pipeline configured with explicit rtpsession for RTCP sender reports")

	s.stream = pipeline
	return nil
}

func (s *SlowCast) runPipeline() error {
	// Dump pipeline
	s.dumpPipelineDot()

	// handle OS signals
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// GStreamer bus messages
	bus := s.stream.GetPipelineBus()
	bus.AddWatch(func(msg *gst.Message) bool {
		switch msg.Type() {
		case gst.MessageEOS:
			fmt.Println("End-Of-Stream reached.")
			s.mainLoop.Quit()
		case gst.MessageError:
			gErr := msg.ParseError()
			fmt.Fprintf(os.Stderr, "GStreamer error: %v\n", gErr)
			s.mainLoop.Quit()
		default:
			fmt.Println(msg) // can be verbose
		}

		return true
	})

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()

	go func() {
		select {
		case <-sigs:
			fmt.Println("Shutting down by signal...")
			s.mainLoop.Quit()
		case <-runCtx.Done():
			return
		}
	}()

	// polling bitrate in a separate goroutine
	go func(ctx context.Context, pipeline *gst.Pipeline) {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		encoder, err := pipeline.GetElementByName("encoder")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Bitrate Polling: Failed to get encoder element: %v\n", err)
			return
		}

		startTime := time.Now()
		fmt.Println("Bitrate Polling: Started.")
		for {
			select {
			case <-ticker.C:
				bitrateVal, errGet := encoder.GetProperty("bitrate")
				if errGet != nil {
					// Log error but continue, as element might be in a transient state
					fmt.Fprintf(os.Stderr, "Bitrate Polling: Failed to get bitrate property: %v\n", errGet)
					continue
				}
				if bitrateUint, ok := bitrateVal.(uint); ok {
					elapsed := time.Since(startTime).Seconds()
					logEntry := map[string]interface{}{
						"type":    "poll_bitrate",
						"elapsed": fmt.Sprintf("%.3f", elapsed),
						"bitrate": bitrateUint,
					}
					jsonEntry, err := json.Marshal(logEntry)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Bitrate Polling: Failed to marshal JSON: %v\n", err)
					} else {
						fmt.Println(string(jsonEntry))
					}
				} else {
					fmt.Fprintf(os.Stderr, "Bitrate Polling: Bitrate property is not of type uint, got %T\n", bitrateVal)
				}
			case <-ctx.Done():
				fmt.Println("Bitrate Polling: Stopped.")
				return
			}
		}
	}(runCtx, s.stream)

	defer func() {
		fmt.Println("Shutting down pipeline...")
		if err := s.stream.SetState(gst.StateNull); err != nil {
			fmt.Fprintf(os.Stderr, "Error setting pipeline to NULL state: %v\n", err)
		}
	}()

	// start PLAYING
	if err := s.stream.SetState(gst.StatePlaying); err != nil {
		return fmt.Errorf("failed to set pipeline to PLAYING state: %w", err)
	}
	fmt.Println("Pipeline is PLAYING. Starting main loop and bitrate polling...")

	return s.mainLoop.RunError()
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func main() {

	versionFlag := flag.Bool("v", false, "Print version and exit")
	debugFlag := flag.Bool("d", false, "Save gst pipeline to DOT file")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("%s v%s - %s\n", appName, appVersion, appDesc)
		os.Exit(0)
	}

	sinkHost := getEnv("UDP_SINK_HOST", "127.0.0.1")
	sinkPortStr := getEnv("UDP_SINK_PORT", "6000")
	srcHost := getEnv("UDP_SRC_HOST", "127.0.0.1")
	srcPortStr := getEnv("UDP_SRC_PORT", "6000")

	sinkPort, err := strconv.Atoi(sinkPortStr)
	if err != nil {
		fmt.Printf("Warning: Invalid port '%s', using default 6000\n", sinkPortStr)
		sinkPort = 6000
	}

	srcPort, err := strconv.Atoi(srcPortStr)
	if err != nil {
		fmt.Printf("Warning: Invalid port '%s', using default 6000\n", srcPortStr)
		srcPort = 6000
	}

	// Create SlowCast
	var slowCast SlowCast
	//nolint:staticcheck
	slow := slowCast.MakeSlowCast(*debugFlag == true)
	slow.mainLoop = glib.NewMainLoop(glib.MainContextDefault(), false)

	if slow.debugEnabled {
		fmt.Println("Debug mode enabled - will generate pipeline DOT file")
	}

	err = slow.createPipeline(sinkHost, srcHost, sinkPort, srcPort)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create pipeline: %v\n", err)
		os.Exit(1)
	}

	// Start the RTCP listener
	go slow.setupRTCPListener(srcHost, srcPort)

	fmt.Printf("Streaming on RTP %s:%d, listening RTCP on %s:%d. Ctrl+C to exit.\n",
		sinkHost, sinkPort, srcHost, srcPort+1)

	if err = slow.runPipeline(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to run pipeline: %v\n", err)
		os.Exit(1)
	}
}
