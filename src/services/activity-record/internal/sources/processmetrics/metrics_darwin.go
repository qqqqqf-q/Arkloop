//go:build darwin

package processmetrics

/*
#cgo LDFLAGS: -lproc
#include <libproc.h>
#include <sys/sysctl.h>
#include <sys/types.h>
#include <mach/mach.h>
#include <mach/host_info.h>
#include <mach/mach_host.h>
#include <net/route.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
	pid_t    pid;
	char     name[64];
	uint64_t rss;
	uint64_t vsz;
	double   cpu_ticks;
} proc_sample_t;

static int list_pids(pid_t **out) {
	int buf_size = proc_listallpids(NULL, 0);
	if (buf_size <= 0) return -1;
	pid_t *pids = (pid_t *)malloc((size_t)buf_size);
	if (!pids) return -1;
	int got = proc_listallpids(pids, buf_size);
	if (got <= 0) { free(pids); return -1; }
	*out = pids;
	return got / (int)sizeof(pid_t);
}

static int sample_proc(pid_t pid, proc_sample_t *out) {
	memset(out, 0, sizeof(*out));
	out->pid = pid;

	struct proc_bsdinfo bsd;
	int n = proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &bsd, sizeof(bsd));
	if (n != sizeof(bsd)) return -1;
	strncpy(out->name, bsd.pbi_name, sizeof(out->name)-1);

	struct proc_taskinfo ti;
	n = proc_pidinfo(pid, PROC_PIDTASKINFO, 0, &ti, sizeof(ti));
	if (n == sizeof(ti)) {
		out->rss       = ti.pti_resident_size;
		out->vsz       = ti.pti_virtual_size;
		out->cpu_ticks = (double)(ti.pti_total_user + ti.pti_total_system);
	}
	return 0;
}

static double host_cpu_ticks(void) {
	host_cpu_load_info_data_t info;
	mach_msg_type_number_t count = HOST_CPU_LOAD_INFO_COUNT;
	if (host_statistics(mach_host_self(), HOST_CPU_LOAD_INFO, (host_info_t)&info, &count) != KERN_SUCCESS) {
		return -1;
	}
	return (double)(info.cpu_ticks[CPU_STATE_USER] +
	                info.cpu_ticks[CPU_STATE_SYSTEM] +
	                info.cpu_ticks[CPU_STATE_NICE] +
	                info.cpu_ticks[CPU_STATE_IDLE]);
}

typedef struct {
	uint64_t rx_bytes;
	uint64_t tx_bytes;
} net_sample_t;

static int sample_net(net_sample_t *out) {
	memset(out, 0, sizeof(*out));
	int mib[] = { CTL_NET, PF_ROUTE, 0, 0, NET_RT_IFLIST2, 0 };
	size_t len = 0;
	if (sysctl(mib, 6, NULL, &len, NULL, 0) < 0) return -1;
	if (len == 0) return 0;

	char *buf = (char *)malloc(len);
	if (!buf) return -1;
	if (sysctl(mib, 6, buf, &len, NULL, 0) < 0) { free(buf); return -1; }

	struct if_msghdr2 *ifm;
	char *p = buf;
	char *end = buf + len;
	while (p + sizeof(struct if_msghdr2) <= end) {
		ifm = (struct if_msghdr2 *)p;
		if (ifm->ifm_type == RTM_IFINFO2) {
			out->rx_bytes += (uint64_t)ifm->ifm_data.ifi_ibytes;
			out->tx_bytes += (uint64_t)ifm->ifm_data.ifi_obytes;
		}
		if (ifm->ifm_msglen == 0) break;
		p += ifm->ifm_msglen;
	}
	free(buf);
	return 0;
}
*/
import "C"
import (
	"fmt"
	"unsafe"
)

type procSample struct {
	PID      int
	Name     string
	RSS      uint64
	VSZ      uint64
	CPUTicks float64
}

type netSample struct {
	RXBytes uint64
	TXBytes uint64
}

type hostSample struct {
	ticks float64
	netRX uint64
	netTX uint64
}

func sampleProcs() ([]procSample, float64, error) {
	var pids *C.pid_t
	count := C.list_pids(&pids)
	if count < 0 {
		return nil, 0, fmt.Errorf("list_pids failed")
	}
	defer C.free(unsafe.Pointer(pids))

	pidSlice := unsafe.Slice((*C.pid_t)(unsafe.Pointer(pids)), count)
	out := make([]procSample, 0, count)

	for _, pid := range pidSlice {
		var cs C.proc_sample_t
		if C.sample_proc(pid, &cs) != 0 {
			continue
		}
		out = append(out, procSample{
			PID:      int(cs.pid),
			Name:     C.GoString(&cs.name[0]),
			RSS:      uint64(cs.rss),
			VSZ:      uint64(cs.vsz),
			CPUTicks: float64(cs.cpu_ticks),
		})
	}

	hostTicks := float64(C.host_cpu_ticks())
	return out, hostTicks, nil
}

func sampleNet() (netSample, error) {
	var cs C.net_sample_t
	if C.sample_net(&cs) != 0 {
		return netSample{}, fmt.Errorf("sample_net failed")
	}
	return netSample{RXBytes: uint64(cs.rx_bytes), TXBytes: uint64(cs.tx_bytes)}, nil
}
