// tftp project main.go
package main

import (
	"container/list"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"reflect"
	"time"
)

type request struct {
	c_addr       *net.UDPAddr // client address
	bytes_recv   int          // bytes received for request
	filename     string       // filename for reading or writing
	mode         uint16       // octet or netascii
	opcode       uint16       // opcode of the request received
	err_code     uint16       // error code if opcode=ERR
	err_msg      string       // error message if opcode=ERR
	blk_no       uint16       // block number if opcode=ACK
	raw_data_pkt []byte       // include complete DATA packet with ||opcode | block # | n-byte data||
	data_size    int          // size of actual data (excluding opcode, block #)
}

type response struct {
	opcode   uint16       // opcode of the response
	err_code uint16       // error code if opcode=ERR
	err_msg  string       // error message if opcode=ERR
	blk_no   uint16       // block number if opcode=DATA
	conn     *net.UDPConn // connection to client for response
	c_addr   *net.UDPAddr // client address
}

// extract byte stream till null (0) byte
func extract_data(p []byte) ([]byte, []byte) {
	for i, b := range p {
		if b == ZERO_BYTE {
			return p[0:i], p[i+1 : len(p)]
		}
	}
	return p, nil
}

func parse_packet(data []byte, bytes_recv int) (*request, string) {
	tftp_req := &request{bytes_recv: bytes_recv}

	// get opcode from the request packet
	tftp_req.opcode = binary.BigEndian.Uint16(data)

	switch tftp_req.opcode {
	case RRQ, WRQ:
		rest_data := data[2:len(data)]
		filename, rest_data := extract_data(rest_data)
		tftp_req.filename = string(filename)
		mode, rest_data := extract_data(rest_data)
		switch string(mode) {
		case "octet":
			tftp_req.mode = OCTET
		case "netascii":
			tftp_req.mode = NETASCII
		default:
			return tftp_req, "Unknown mode " + string(mode)
		}

	case ERR:
		rest_data := data[2:len(data)]
		tftp_req.err_code = binary.BigEndian.Uint16(rest_data)
		rest_data = rest_data[2:len(rest_data)]
		err_msg, rest_data := extract_data(rest_data)
		tftp_req.err_msg = string(err_msg)
		return tftp_req, "Received Error code " + string(tftp_req.err_code) + ": " + tftp_req.err_msg

	case DAT:
		tftp_req.raw_data_pkt = data[:len(data)]
		rest_data := data[2:len(data)]
		tftp_req.blk_no = binary.BigEndian.Uint16(rest_data)
		rest_data = rest_data[2:len(rest_data)]
		tftp_req.data_size = len(rest_data)

	case ACK:
		rest_data := data[2:len(data)]
		tftp_req.blk_no = binary.BigEndian.Uint16(rest_data)
	}
	return tftp_req, ""
}

func send_response(tftp_res response, data []byte) {
	switch tftp_res.opcode {
	case ACK:
		var buf []byte = make([]byte, 4)
		offset := 0
		binary.BigEndian.PutUint16(buf[offset:], tftp_res.opcode)
		offset = offset + 2
		binary.BigEndian.PutUint16(buf[offset:], tftp_res.blk_no)
		_, err := tftp_res.conn.Write(buf)
		if err != nil {
			fmt.Println("Error writing to UDP: ", err)
		}
	case ERR:
		var buf []byte = make([]byte, MAX_BUFFER_SIZE)
		offset := 0
		binary.BigEndian.PutUint16(buf[offset:], tftp_res.opcode)
		offset = offset + 2
		binary.BigEndian.PutUint16(buf[offset:], tftp_res.err_code)
		offset = offset + 2
		bytes_copied := copy(buf[offset:], tftp_res.err_msg)
		offset = offset + bytes_copied
		binary.BigEndian.PutUint16(buf[offset:], 0)
		offset = offset + 2

		_, err := tftp_res.conn.Write(buf[:offset])
		if err != nil {
			fmt.Println("Error writing to UDP: ", err)
		}
	case DAT:
		_, err := tftp_res.conn.Write(data)
		if err != nil {
			fmt.Println("Error writing to UDP: ", err)
		}
	}
	return
}

func handle_rrq(files map[string]*list.List, orig_tftp_req *request) {
	rrq_addr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		fmt.Println("Error resolving UDP address: ", err)
		return
	}
	rrq_conn, err := net.DialUDP("udp", rrq_addr, orig_tftp_req.c_addr)
	if err != nil {
		fmt.Println("Error dialing UDP to client: ", err)
		return
	}
	defer rrq_conn.Close()

	tmp_tftp_res := response{conn: rrq_conn, c_addr: orig_tftp_req.c_addr}

	// check if file exists
	if files[orig_tftp_req.filename] == nil {
		tmp_tftp_res.opcode = ERR
		tmp_tftp_res.err_code = NOT_FOUND
		tmp_tftp_res.err_msg = string("Can not continue with file read as file does not exist at server side.")
		send_response(tmp_tftp_res, nil)
		fmt.Println("File not present! File - ", orig_tftp_req.filename)
		return
	}

	blk_no := uint16(1)
	retry_cnt := 0
	ll := files[orig_tftp_req.filename]
	tmp_tftp_res.opcode = DAT
	var pkt_buf []byte = make([]byte, MAX_BUFFER_SIZE)
	i := 0

	// iterate through the linked list to send the data packets
	for e := ll.Front(); e != nil; i++ {
		if retry_cnt >= 3 {
			fmt.Println("Timeout while reading data from connection.")
			return
		}
		data := e.Value.([]byte)
		send_response(tmp_tftp_res, data)
		retry_cnt = retry_cnt + 1

		// set timeout of 1 sec for receiving packet from client
		tmp_tftp_res.conn.SetReadDeadline(time.Now().Add(TIMEOUT * time.Second))
		n, raddr, err := tmp_tftp_res.conn.ReadFromUDP(pkt_buf)
		if err != nil {
			e_tout, status := err.(net.Error)
			if status && e_tout.Timeout() {
				continue
			}
			fmt.Println("Error reading data from connection: ", err)
			tmp_tftp_res.opcode = ERR
			tmp_tftp_res.err_code = UNKNOWN_ERROR
			tmp_tftp_res.err_msg = string("Error reading from UDP at server side.")
			send_response(tmp_tftp_res, nil)
			return
		}
		curr_tftp_req, err_msg := parse_packet(pkt_buf[:n], n)
		if len(err_msg) > 0 {
			if curr_tftp_req.opcode == ERR {
				fmt.Println("Error received from client error: ", err_msg)
				return
			}
			fmt.Println("Encountered error while parsing the tftp packet: ", err_msg)
			continue
		}
		curr_tftp_req.c_addr = raddr
		// check if appropriate sequence ACK is received
		if curr_tftp_req.opcode == ACK &&
			curr_tftp_req.blk_no == blk_no &&
			reflect.DeepEqual(curr_tftp_req.c_addr, tmp_tftp_res.c_addr) {
			blk_no = blk_no + 1
			retry_cnt = 0
			e = e.Next()
		}
	}
	fmt.Println("File transfer completed! Sent file - ", orig_tftp_req.filename)
}

func handle_wrq(files map[string]*list.List, orig_tftp_req *request) {

	wrq_addr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		fmt.Println("Error resolving UDP address: ", err)
		return
	}
	wrq_conn, err := net.DialUDP("udp", wrq_addr, orig_tftp_req.c_addr)
	if err != nil {
		fmt.Println("Error dialing UDP to client: ", err)
		return
	}
	tmp_tftp_res := response{conn: wrq_conn, c_addr: orig_tftp_req.c_addr}

	// check if file already exists
	if files[orig_tftp_req.filename] != nil {
		tmp_tftp_res.opcode = ERR
		tmp_tftp_res.err_code = ALREADY_EXISTS
		tmp_tftp_res.err_msg = string("Can not continue with file write as file already exists at server side.")
		send_response(tmp_tftp_res, nil)
		fmt.Println("File already present! File - ", orig_tftp_req.filename)
		return
	}

	// send first ack for wrq with block #0
	blk_no := uint16(0)
	tmp_tftp_res.opcode = ACK
	tmp_tftp_res.blk_no = blk_no
	blk_no = blk_no + 1
	send_response(tmp_tftp_res, nil)

	fdata_list := list.New()
	var pkt_buf []byte = make([]byte, MAX_BUFFER_SIZE)
	retry_cnt := 0
	for {
		// set timeout of 1 sec for receiving packet from client
		tmp_tftp_res.conn.SetReadDeadline(time.Now().Add(TIMEOUT * time.Second))
		n, raddr, err := tmp_tftp_res.conn.ReadFromUDP(pkt_buf)
		if err != nil {
			e_tout, status := err.(net.Error)
			if status && e_tout.Timeout() {
				if retry_cnt >= 3 {
					fmt.Println("Timeout while reading data from connection.")
					return
				}
				// resend the previous response
				send_response(tmp_tftp_res, nil)
				retry_cnt = retry_cnt + 1
				continue
			}
			fmt.Println("Error reading data from connection: ", err)
			tmp_tftp_res.opcode = ERR
			tmp_tftp_res.err_code = UNKNOWN_ERROR
			tmp_tftp_res.err_msg = string("Error reading from UDP at server side.")
			send_response(tmp_tftp_res, nil)
			return
		}
		curr_tftp_req, err_msg := parse_packet(pkt_buf[:n], n)
		if len(err_msg) > 0 {
			if curr_tftp_req.opcode == ERR {
				fmt.Println("Error received from client - error: ", err_msg)
				return
			}
			fmt.Println("Encountered error while parsing the tftp packet: ", err_msg)
			continue
		}
		curr_tftp_req.c_addr = raddr

		// check if appropriate sequence data packet is received and store it in linked-list
		if curr_tftp_req.opcode == DAT &&
			curr_tftp_req.blk_no == blk_no &&
			reflect.DeepEqual(curr_tftp_req.c_addr, tmp_tftp_res.c_addr) {
			raw_data := make([]byte, curr_tftp_req.data_size+4)
			copy(raw_data, curr_tftp_req.raw_data_pkt)
			fdata_list.PushBack(raw_data)
			tmp_tftp_res.opcode = ACK
			tmp_tftp_res.blk_no = blk_no
			retry_cnt = 0                    // reset retry count
			send_response(tmp_tftp_res, nil) // ack the received block number
			blk_no = blk_no + 1
		}
		// check if this is the last received data packet
		if curr_tftp_req.data_size < BLOCKSIZE {
			fmt.Println("File transfer completed! Received file - ", orig_tftp_req.filename)
			break
		}
	}
	// add entry in to map once a complete transfer of file is done
	// so that the partially transferred file will not be available for reading
	files[orig_tftp_req.filename] = fdata_list
	return
}

func handle_request(files map[string]*list.List, tftp_req *request) {
	switch tftp_req.opcode {
	case WRQ:
		go handle_wrq(files, tftp_req)

	case RRQ:
		go handle_rrq(files, tftp_req)
	}
	return
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Missing arguments. Please check usage.")
		fmt.Println("Usage: ./tftp localhost:<port#>")
		os.Exit(1)
	}
	addr, err := net.ResolveUDPAddr("udp", os.Args[1])
	if err != nil {
		fmt.Println("Failed to resolve address.", err)
		os.Exit(1)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		fmt.Println("Failed listen UDP.", err)
		os.Exit(1)
	}
	defer conn.Close()

	files := make(map[string]*list.List)
	var pkt_buf []byte = make([]byte, MAX_BUFFER_SIZE)

	// loop infinitely for accepting requests from tftp client
	for {
		n, raddr, err := conn.ReadFromUDP(pkt_buf)
		if err != nil || n <= 0 {
			fmt.Println("Error reading data from connection: ", err)
			return
		}
		tftp_req, err_msg := parse_packet(pkt_buf[:n], n)
		if len(err_msg) > 0 {
			fmt.Println("Encountered error: ", err_msg)
			continue
		}
		tftp_req.c_addr = raddr

		if tftp_req.opcode == WRQ || tftp_req.opcode == RRQ {
			handle_request(files, tftp_req)
		}
	}
}
