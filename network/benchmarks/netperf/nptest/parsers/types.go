package parsers

type IperfCommandOutputStart struct {
	Connected []struct {
		Socket     int    `json:"socket"`
		LocalHost  string `json:"local_host"`
		LocalPort  int    `json:"local_port"`
		RemoteHost string `json:"remote_host"`
		RemotePort int    `json:"remote_port"`
	} `json:"connected"`
	TCPMss    int `json:"tcp_mss"`
	TestStart struct {
		Protocol   string `json:"protocol"`
		NumStreams int    `json:"num_streams"`
		BlkSize    int    `json:"blksize"`
		Duration   int    `json:"duration"`
	} `json:"test_start"`
}

type IperfCPUUtilizationPercent struct {
	HostTotal   float64 `json:"host_total"`
	RemoteTotal float64 `json:"remote_total"`
}

type IperfTCPSenderSumStats struct {
	Seconds       float64 `json:"seconds"`
	BitsPerSecond float64 `json:"bits_per_second"`
	Bytes         int     `json:"bytes"`
	Retransmits   int     `json:"retransmits"`
}

type IperfTCPReceiverSumStats struct {
	Seconds       float64 `json:"seconds"`
	BitsPerSecond float64 `json:"bits_per_second"`
	Bytes         int     `json:"bytes"`
}

type IperfTCPCommandOutput struct {
	Start     IperfCommandOutputStart `json:"start"`
	Intervals []struct {
		Streams []struct {
			BitsPerSecond float64 `json:"bits_per_second"`
			Bytes         int     `json:"bytes"`
			Rtt           uint    `json:"rtt"`
			Seconds       float64 `json:"seconds"`
			Retransmits   int     `json:"retransmits"`
		} `json:"streams"`
		Sum IperfTCPSenderSumStats `json:"sum"`
	} `json:"intervals"`
	End struct {
		Streams []struct {
			Sender struct {
				Seconds       float64 `json:"seconds"`
				BitsPerSecond float64 `json:"bits_per_second"`
				Bytes         int     `json:"bytes"`
				Retransmits   int     `json:"retransmits"`
				MaxRtt        uint    `json:"max_rtt"`
				MinRtt        uint    `json:"min_rtt"`
				MeanRtt       uint    `json:"mean_rtt"`
			} `json:"sender"`
			Reciever IperfTCPReceiverSumStats `json:"receiver"`
		} `json:"streams"`
		SumSent               IperfTCPSenderSumStats     `json:"sum_sent"`
		SumReceived           IperfTCPReceiverSumStats   `json:"sum_received"`
		CPUUtilizationPercent IperfCPUUtilizationPercent `json:"cpu_utilization_percent"`
	} `json:"end"`
}

type IperfUDPIntervalObject struct {
	BitsPerSecond float64 `json:"bits_per_second"`
	Bytes         int     `json:"bytes"`
	Packets       int     `json:"packets"`
	Seconds       float64 `json:"seconds"`
}

type IperfUDPCommandOutput struct {
	Start    IperfCommandOutputStart `json:"start"`
	Interval []struct {
		Streams []IperfUDPIntervalObject `json:"streams"`
		Sum     IperfUDPIntervalObject   `json:"sum"`
	} `json:"intervals"`
	End struct {
		Streams []struct {
			UDP struct {
				Bytes             int     `json:"bytes"`
				BitsPerSecond     float64 `json:"bits_per_second"`
				Jitter            float64 `json:"jitter_ms"`
				LostPackets       int     `json:"lost_packets"`
				Packets           int     `json:"packets"`
				LostPercent       float64 `json:"lost_percent"`
				OutOfOrderPackets int     `json:"out_of_order"`
			} `json:"udp"`
		} `json:"streams"`
		Sum struct {
			Bytes         int     `json:"bytes"`
			BitsPerSecond float64 `json:"bits_per_second"`
			Jitter        float64 `json:"jitter_ms"`
			LostPackets   int     `json:"lost_packets"`
			Packets       int     `json:"packets"`
			LostPercent   float64 `json:"lost_percent"`
		} `json:"sum"`
		CPUUtilizationPercent IperfCPUUtilizationPercent `json:"cpu_utilization_percent"`
	} `json:"end"`
}

type IperfTestInfo struct {
	Protocol string `json:"protocol"`
	Streams  int    `json:"streams"`
	BlkSize  int    `json:"blksize"`
	Duration int    `json:"duration"`
	Mss      int    `json:"mss,omitempty"`
}

type IperfTCPParsedResult struct {
	TestInfo          IperfTestInfo              `json:"test_info"`
	TotalThroughput   float64                    `json:"total_throughput"`
	MeanRoundTripTime float64                    `json:"mean_rtt"`
	MinRoundTripTime  uint                       `json:"min_rtt"`
	MaxRoundTripTime  uint                       `json:"max_rtt"`
	Retransmits       int                        `json:"retransmits"`
	CPUUtilization    IperfCPUUtilizationPercent `json:"cpu_utilization"`
}

type IperfUDPParsedResult struct {
	TestInfo               IperfTestInfo              `json:"test_info"`
	TotalThroughput        float64                    `json:"total_throughput"`
	Jitter                 float64                    `json:"jitter_ms"`
	LostPackets            int                        `json:"lost_packets"`
	TotalPackets           int                        `json:"total_packets"`
	LostPercent            float64                    `json:"lost_percent"`
	TotalOutOfOrderPackets int                        `json:"out_of_order_packets"`
	CPUUtilization         IperfCPUUtilizationPercent `json:"cpu_utilization"`
}
