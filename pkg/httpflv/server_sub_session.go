// Copyright 2019, Chef.  All rights reserved.
// https://github.com/q191201771/lal
//
// Use of this source code is governed by a MIT-style license
// that can be found in the License file.
//
// Author: Chef (191201771@qq.com)

package httpflv

import (
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/q191201771/lal/pkg/base"

	"github.com/q191201771/naza/pkg/nazahttp"

	"github.com/q191201771/naza/pkg/connection"

	"github.com/q191201771/naza/pkg/nazalog"
)

var flvHTTPResponseHeader []byte

type SubSession struct {
	UniqueKey string

	StartTick              int64
	AppName                string
	StreamName             string
	RawQuery               string
	StreamNameWithRawQuery string
	Headers                map[string]string

	IsFresh bool

	conn         connection.Connection
	prevConnStat connection.Stat
	staleStat    *connection.Stat
	stat         base.StatSub
}

func NewSubSession(conn net.Conn) *SubSession {
	uk := base.GenUniqueKey(base.UKPFLVSubSession)
	s := &SubSession{
		UniqueKey: uk,
		IsFresh:   true,
		conn: connection.New(conn, func(option *connection.Option) {
			option.ReadBufSize = readBufSize
			option.WriteChanSize = wChanSize
			option.WriteTimeoutMS = subSessionWriteTimeoutMS
		}),
		stat: base.StatSub{
			StatSession: base.StatSession{
				Protocol:   base.ProtocolHTTPFLV,
				SessionID:  uk,
				StartTime:  time.Now().Format("2006-01-02 15:04:05.999"),
				RemoteAddr: conn.RemoteAddr().String(),
			},
		},
	}
	nazalog.Infof("[%s] lifecycle new httpflv SubSession. session=%p, remote addr=%s", uk, s, conn.RemoteAddr().String())
	return s
}

// TODO chef: read request timeout
func (session *SubSession) ReadRequest() (err error) {
	session.StartTick = time.Now().Unix()

	defer func() {
		if err != nil {
			session.Dispose()
		}
	}()

	var (
		requestLine string
		method      string
	)
	if requestLine, session.Headers, err = nazahttp.ReadHTTPHeader(session.conn); err != nil {
		return
	}
	if method, session.StreamNameWithRawQuery, _, err = nazahttp.ParseHTTPRequestLine(requestLine); err != nil {
		return
	}
	if method != "GET" {
		err = ErrHTTPFLV
		return
	}

	var urlObj *url.URL
	if urlObj, err = url.Parse(session.StreamNameWithRawQuery); err != nil {
		return
	}
	if !strings.HasSuffix(urlObj.Path, ".flv") {
		err = ErrHTTPFLV
		return
	}

	items := strings.Split(urlObj.Path, "/")
	if len(items) != 3 {
		err = ErrHTTPFLV
		return
	}
	session.AppName = items[1]
	items = strings.Split(items[2], ".")
	if len(items) < 2 {
		err = ErrHTTPFLV
		return
	}
	session.StreamName = items[0]
	session.RawQuery = urlObj.RawQuery

	return nil
}

func (session *SubSession) RunLoop() error {
	buf := make([]byte, 128)
	_, err := session.conn.Read(buf)
	return err
}

func (session *SubSession) WriteHTTPResponseHeader() {
	nazalog.Debugf("[%s] > W http response header.", session.UniqueKey)
	session.WriteRawPacket(flvHTTPResponseHeader)
}

func (session *SubSession) WriteFLVHeader() {
	nazalog.Debugf("[%s] > W http flv header.", session.UniqueKey)
	session.WriteRawPacket(FLVHeader)
}

func (session *SubSession) WriteTag(tag *Tag) {
	session.WriteRawPacket(tag.Raw)
}

func (session *SubSession) WriteRawPacket(pkt []byte) {
	_, _ = session.conn.Write(pkt)
}

func (session *SubSession) Dispose() {
	nazalog.Infof("[%s] lifecycle dispose httpflv SubSession.", session.UniqueKey)
	_ = session.conn.Close()
}

func (session *SubSession) GetStat() base.StatSub {
	currStat := session.conn.GetStat()
	session.stat.ReadBytesSum = currStat.ReadBytesSum
	session.stat.WroteBytesSum = currStat.WroteBytesSum
	return session.stat
}

func (session *SubSession) UpdateStat(interval uint32) {
	currStat := session.conn.GetStat()
	var diffStat connection.Stat
	diffStat.WroteBytesSum = currStat.WroteBytesSum - session.prevConnStat.WroteBytesSum
	session.stat.Bitrate = int(diffStat.WroteBytesSum * 8 / 1024 / uint64(interval))
	session.prevConnStat = currStat
}

func (session *SubSession) IsAlive(interval uint32) (ret bool) {
	currStat := session.conn.GetStat()
	if session.staleStat == nil {
		session.staleStat = new(connection.Stat)
		*session.staleStat = currStat
		return true
	}

	ret = !(currStat.WroteBytesSum-session.staleStat.WroteBytesSum == 0)
	*session.staleStat = currStat
	return ret
}

func (session *SubSession) RemoteAddr() string {
	return session.conn.RemoteAddr().String()
}

func init() {
	flvHTTPResponseHeaderStr := "HTTP/1.1 200 OK\r\n" +
		"Server: " + base.LALHTTPFLVSubSessionServer + "\r\n" +
		"Cache-Control: no-cache\r\n" +
		"Content-Type: video/x-flv\r\n" +
		"Connection: close\r\n" +
		"Expires: -1\r\n" +
		"Pragma: no-cache\r\n" +
		"Access-Control-Allow-Origin: *\r\n" +
		"\r\n"

	flvHTTPResponseHeader = []byte(flvHTTPResponseHeaderStr)
}
