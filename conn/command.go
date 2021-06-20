package conn

import (
	"fmt"
	newamf "github.com/gwuhaolin/livego/protocol/amf"
	"github.com/gwuhaolin/livego/utils/pio"
	"github.com/haroldleong/easylive/command"
	"github.com/haroldleong/easylive/consts"
	"github.com/haroldleong/easylive/format/flv/amf"
	"github.com/haroldleong/easylive/util"
	log "github.com/sirupsen/logrus"
)

func (c *Conn) HandleChunk(cs *ChunkStream) (err error) {
	var cmd *command.Command
	switch cs.TypeID {
	case consts.MsgTypeIDSetChunkSize:
		c.readMaxChunkSize = int(pio.U32BE(cs.Data))
		log.Debugf("HandleChunk.type MsgTypeIDSetChunkSize,size:%d", c.readMaxChunkSize)
		return nil
	case consts.MsgTypeIDWindowAckSize:
		c.remoteWindowAckSize = pio.U32BE(cs.Data)
		log.Debugf("HandleChunk.type MsgTypeIDWindowAckSize,size:%d", c.remoteWindowAckSize)
		return nil
	case consts.MsgTypeIDCommandMsgAMF3:
		log.Debugf("HandleChunk.type MsgTypeIDCommandMsgAMF3")
		if len(cs.Data) < 1 {
			err = fmt.Errorf("rtmp: short packet of CommandMsgAMF3")
			return
		}
		// skip first byte
		if cmd, err = c.handleCommandMsgAMF0(cs.Data[1:]); err != nil {
			return
		}
	case consts.MsgTypeIDCommandMsgAMF0:
		log.Debugf("HandleChunk.type MsgTypeIDCommandMsgAMF0")
		if cmd, err = c.handleCommandMsgAMF0(cs.Data); err != nil {
			return err
		}
	case consts.MsgTypeIDUserControl:
		eventType := pio.U16BE(cs.Data)
		log.Debugf("HandleChunk.type MsgTypeIDUserControl.eventType:%d", eventType)
		return nil
	default:
		log.Warnf("HandleChunk.ignore type.id:%d", cs.TypeID)
	}
	if cmd == nil {
		return fmt.Errorf("no cmd handler,typeID:%v", cs.TypeID)
	}
	log.Infof("HandleChunk.get cmd.%s", util.JSON(cmd))
	return c.processCMD(cs, cmd)
}

func (c *Conn) processCMD(cs *ChunkStream, cmd *command.Command) error {
	switch cmd.CommandName {
	case consts.CmdConnect:
		if err := c.connect(cmd); err != nil {
			return err
		}
		if err := c.connectResp(cs, cmd); err != nil {
			return err
		}
		log.Infof("processCMD.cmdConnect finish")
	case consts.CmdCreateStream:
		if err := c.createStreamResp(cs, cmd); err != nil {
			return err
		}
		log.Infof("processCMD.cmdCreateStream finish")
	case consts.CmdPublish:
		if len(cmd.CommandParams) < 1 {
			return fmt.Errorf("rtmp: publish params invalid")
		}
		// playPath := cmd.CommandName[0]
		// playType := cmd.CommandName[1]
		// “live”、”record”、”append”中的一种。
		// live表示该推流文件不会在服务器端存储；
		// record表示该推流的文件会在服务器应用程序下的子目录下保存以便后续播放，如果文件已经存在的话删除原来所有的内容重新写入；
		// append也会将推流数据保存在服务器端，如果文件不存在的话就会建立一个新文件写入，如果对应该流的文件已经存在的话保存原来的数据，在文件末尾接着写入
		log.Debugf("processCMD.CommandParams:%v", util.JSON(cmd.CommandParams))
		// rtmp适合于flv container.如果是mp4会出错
		if err := c.publishResp(cs, cmd); err != nil {
			return err
		}
		c.messageDone = true
		c.ConnType = ConnectionTypePublish
	case consts.CmdPlay:
		if len(cmd.CommandParams) < 1 {
			return fmt.Errorf("rtmp: play params invalid")
		}
		playPath, _ := cmd.CommandParams[0].(string)
		log.Debugf("processCMD.cmdPlay.playPath:%s", playPath)
		if err := c.markStreamBegin(); err != nil {
			return err
		}
		if err := c.playResp(cs, cmd); err != nil {
			return err
		}
		c.messageDone = true
		c.ConnType = ConnectionTypePull
	case consts.CmdFcpublish:
		log.Debugf("processCMD.cmdFcpublish")
	case consts.CmdReleaseStream:
		/*
			音频、视频、元数据均通过createStream创建的数据通道进行交互，一般会在createStream之前先来一次releaseStream
			因为我们每次开启新的流程，并不能确保之前的流程是否正常走完，是否出现了异常情况，异常的情况是否已经处理等等
		*/
		log.Debugf("processCMD.cmdReleaseStream")
	case consts.CmdFCUnPublish:
		log.Debugf("processCMD.cmdFCUnPublish")
	case consts.CmdDeleteStream:
		log.Debugf("processCMD.cmdDeleteStream")
	case consts.CmdGetStreamLength:
		log.Debugf("processCMD.cmdGetStreamLength")
	default:
		log.Warnf("processCMD.no support command:%s", cmd.CommandName)
	}
	return nil
}

func (c *Conn) connect(cmd *command.Command) error {
	if app, ok := cmd.CommandObj["app"]; ok {
		c.ConnInfo.App = app.(string)
	}
	if flashVer, ok := cmd.CommandObj["flashVer"]; ok {
		c.ConnInfo.Flashver = flashVer.(string)
	}
	if tcurl, ok := cmd.CommandObj["tcUrl"]; ok {
		c.ConnInfo.TcUrl = tcurl.(string)
	}
	if encoding, ok := cmd.CommandObj["objectEncoding"]; ok {
		c.ConnInfo.ObjectEncoding = int(encoding.(float64))
	}
	return nil
}

func (c *Conn) connectResp(cur *ChunkStream, cmd *command.Command) error {
	cs := c.NewWindowAckSize(2500000)
	c.Write(&cs)
	cs = c.NewSetPeerBandwidth(2500000)
	c.Write(&cs)
	cs = c.NewSetChunkSize(uint32(1024))
	c.Write(&cs)

	resp := make(newamf.Object)
	resp["fmsVer"] = "FMS/3,0,1,123"
	resp["capabilities"] = 31

	event := make(newamf.Object)
	event["level"] = "status"
	event["code"] = "NetConnection.Connect.Success"
	event["description"] = "Connection succeeded."
	event["objectEncoding"] = c.ConnInfo.ObjectEncoding
	return c.writeCommandMsg(cur.CSID, cur.StreamID, "_result", cmd.CommandTransId, resp, event)
}

func (c *Conn) publishResp(cs *ChunkStream, cmd *command.Command) error {
	event := make(newamf.Object)
	event["level"] = "status"
	event["code"] = "NetStream.Publish.Start"
	event["description"] = "Start publising."
	return c.writeCommandMsg(cs.CSID, cs.StreamID, "onStatus", 0, nil, event)
}

func (c *Conn) markStreamBegin() error {
	ret := c.userControlMsg(uint32(consts.UserControlStreamIsRecorded), 4)
	for i := 0; i < 4; i++ {
		ret.Data[2+i] = byte(1 >> uint32((3-i)*8) & 0xff)
	}
	c.Write(&ret)

	ret = c.userControlMsg(uint32(consts.UserControlStreamBegin), 4)
	for i := 0; i < 4; i++ {
		ret.Data[2+i] = byte(1 >> uint32((3-i)*8) & 0xff)
	}
	return c.Write(&ret)
}

func (c *Conn) playResp(cs *ChunkStream, cmd *command.Command) error {
	event := make(newamf.Object)

	event["level"] = "status"
	event["code"] = "NetStream.Play.Reset"
	event["description"] = "Playing and resetting stream."
	if err := c.writeCommandMsg(cs.CSID, cs.StreamID, "onStatus", 0, nil, event); err != nil {
		return err
	}

	event["level"] = "status"
	event["code"] = "NetStream.Play.Start"
	event["description"] = "Started playing stream."
	if err := c.writeCommandMsg(cs.CSID, cs.StreamID, "onStatus", 0, nil, event); err != nil {
		return err
	}

	event["level"] = "status"
	event["code"] = "NetStream.Data.Start"
	event["description"] = "Started playing stream."
	if err := c.writeCommandMsg(cs.CSID, cs.StreamID, "onStatus", 0, nil, event); err != nil {
		return err
	}

	event["level"] = "status"
	event["code"] = "NetStream.Play.PublishNotify"
	event["description"] = "Started playing notify."
	if err := c.writeCommandMsg(cs.CSID, cs.StreamID, "onStatus", 0, nil, event); err != nil {
		return err
	}

	c.bufWriter.Flush()
	return nil
}

func (c *Conn) createStreamResp(cs *ChunkStream, cmd *command.Command) error {
	return c.writeCommandMsg(cs.CSID, cs.StreamID, "_result", cmd.CommandTransId, nil, 1)
}

func (c *Conn) handleCommandMsgAMF0(b []byte) (cmd *command.Command, err error) {
	/*	{
		"GotCommand": true,
		"CommandName": "connect",
		"CommandTransId": 1,
		"CommandObj": {
			"app": "movie",
			"flashVer": "FMLE/3.0 (compatible; Lavf58.76.100)",
			"tcUrl": "rtmp://localhost:1936/movie",
			"type": "nonprivate"
		},
		"CommandParams": []
	}*/
	var name, transid, obj interface{}
	var (
		size int
		n    int
	)

	cmd = &command.Command{}

	if name, size, err = amf.ParseAMF0Val(b[n:]); err != nil {
		return
	}
	n += size
	if transid, size, err = amf.ParseAMF0Val(b[n:]); err != nil {
		return
	}
	n += size
	if obj, size, err = amf.ParseAMF0Val(b[n:]); err != nil {
		return
	}
	n += size

	var ok bool
	if cmd.CommandName, ok = name.(string); !ok {
		err = fmt.Errorf("rtmp: CommandMsgAMF0 command is not string")
		return
	}
	cmd.CommandTransId, _ = transid.(float64)
	cmd.CommandObj, _ = obj.(amf.AMFMap)
	cmd.CommandParams = []interface{}{}

	for n < len(b) {
		if obj, size, err = amf.ParseAMF0Val(b[n:]); err != nil {
			return
		}
		n += size
		cmd.CommandParams = append(cmd.CommandParams, obj)
	}
	if n < len(b) {
		err = fmt.Errorf("rtmp: CommandMsgAMF0 left bytes=%d", len(b)-n)
		return
	}
	cmd.GotCommand = true
	return
}
