// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.35.1
// 	protoc        v3.12.4
// source: bc.proto

package pb

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type Empty struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields
}

func (x *Empty) Reset() {
	*x = Empty{}
	mi := &file_bc_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Empty) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Empty) ProtoMessage() {}

func (x *Empty) ProtoReflect() protoreflect.Message {
	mi := &file_bc_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Empty.ProtoReflect.Descriptor instead.
func (*Empty) Descriptor() ([]byte, []int) {
	return file_bc_proto_rawDescGZIP(), []int{0}
}

type Message struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Text string `protobuf:"bytes,1,opt,name=text,proto3" json:"text,omitempty"`
}

func (x *Message) Reset() {
	*x = Message{}
	mi := &file_bc_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Message) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Message) ProtoMessage() {}

func (x *Message) ProtoReflect() protoreflect.Message {
	mi := &file_bc_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Message.ProtoReflect.Descriptor instead.
func (*Message) Descriptor() ([]byte, []int) {
	return file_bc_proto_rawDescGZIP(), []int{1}
}

func (x *Message) GetText() string {
	if x != nil {
		return x.Text
	}
	return ""
}

type ViewportSize struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Width  int32 `protobuf:"varint,1,opt,name=width,proto3" json:"width,omitempty"`
	Height int32 `protobuf:"varint,2,opt,name=height,proto3" json:"height,omitempty"`
}

func (x *ViewportSize) Reset() {
	*x = ViewportSize{}
	mi := &file_bc_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ViewportSize) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ViewportSize) ProtoMessage() {}

func (x *ViewportSize) ProtoReflect() protoreflect.Message {
	mi := &file_bc_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ViewportSize.ProtoReflect.Descriptor instead.
func (*ViewportSize) Descriptor() ([]byte, []int) {
	return file_bc_proto_rawDescGZIP(), []int{2}
}

func (x *ViewportSize) GetWidth() int32 {
	if x != nil {
		return x.Width
	}
	return 0
}

func (x *ViewportSize) GetHeight() int32 {
	if x != nil {
		return x.Height
	}
	return 0
}

type Coordinate struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	X int32 `protobuf:"varint,1,opt,name=x,proto3" json:"x,omitempty"`
	Y int32 `protobuf:"varint,2,opt,name=y,proto3" json:"y,omitempty"`
}

func (x *Coordinate) Reset() {
	*x = Coordinate{}
	mi := &file_bc_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Coordinate) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Coordinate) ProtoMessage() {}

func (x *Coordinate) ProtoReflect() protoreflect.Message {
	mi := &file_bc_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Coordinate.ProtoReflect.Descriptor instead.
func (*Coordinate) Descriptor() ([]byte, []int) {
	return file_bc_proto_rawDescGZIP(), []int{3}
}

func (x *Coordinate) GetX() int32 {
	if x != nil {
		return x.X
	}
	return 0
}

func (x *Coordinate) GetY() int32 {
	if x != nil {
		return x.Y
	}
	return 0
}

type Text struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Content string `protobuf:"bytes,1,opt,name=content,proto3" json:"content,omitempty"`
}

func (x *Text) Reset() {
	*x = Text{}
	mi := &file_bc_proto_msgTypes[4]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Text) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Text) ProtoMessage() {}

func (x *Text) ProtoReflect() protoreflect.Message {
	mi := &file_bc_proto_msgTypes[4]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Text.ProtoReflect.Descriptor instead.
func (*Text) Descriptor() ([]byte, []int) {
	return file_bc_proto_rawDescGZIP(), []int{4}
}

func (x *Text) GetContent() string {
	if x != nil {
		return x.Content
	}
	return ""
}

type Url struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Url string `protobuf:"bytes,1,opt,name=url,proto3" json:"url,omitempty"`
}

func (x *Url) Reset() {
	*x = Url{}
	mi := &file_bc_proto_msgTypes[5]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Url) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Url) ProtoMessage() {}

func (x *Url) ProtoReflect() protoreflect.Message {
	mi := &file_bc_proto_msgTypes[5]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Url.ProtoReflect.Descriptor instead.
func (*Url) Descriptor() ([]byte, []int) {
	return file_bc_proto_rawDescGZIP(), []int{5}
}

func (x *Url) GetUrl() string {
	if x != nil {
		return x.Url
	}
	return ""
}

type Screenshot struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Data []byte `protobuf:"bytes,1,opt,name=data,proto3" json:"data,omitempty"`
}

func (x *Screenshot) Reset() {
	*x = Screenshot{}
	mi := &file_bc_proto_msgTypes[6]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Screenshot) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Screenshot) ProtoMessage() {}

func (x *Screenshot) ProtoReflect() protoreflect.Message {
	mi := &file_bc_proto_msgTypes[6]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Screenshot.ProtoReflect.Descriptor instead.
func (*Screenshot) Descriptor() ([]byte, []int) {
	return file_bc_proto_rawDescGZIP(), []int{6}
}

func (x *Screenshot) GetData() []byte {
	if x != nil {
		return x.Data
	}
	return nil
}

var File_bc_proto protoreflect.FileDescriptor

var file_bc_proto_rawDesc = []byte{
	0x0a, 0x08, 0x62, 0x63, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x02, 0x70, 0x62, 0x22, 0x07,
	0x0a, 0x05, 0x45, 0x6d, 0x70, 0x74, 0x79, 0x22, 0x1d, 0x0a, 0x07, 0x4d, 0x65, 0x73, 0x73, 0x61,
	0x67, 0x65, 0x12, 0x12, 0x0a, 0x04, 0x74, 0x65, 0x78, 0x74, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09,
	0x52, 0x04, 0x74, 0x65, 0x78, 0x74, 0x22, 0x3c, 0x0a, 0x0c, 0x56, 0x69, 0x65, 0x77, 0x70, 0x6f,
	0x72, 0x74, 0x53, 0x69, 0x7a, 0x65, 0x12, 0x14, 0x0a, 0x05, 0x77, 0x69, 0x64, 0x74, 0x68, 0x18,
	0x01, 0x20, 0x01, 0x28, 0x05, 0x52, 0x05, 0x77, 0x69, 0x64, 0x74, 0x68, 0x12, 0x16, 0x0a, 0x06,
	0x68, 0x65, 0x69, 0x67, 0x68, 0x74, 0x18, 0x02, 0x20, 0x01, 0x28, 0x05, 0x52, 0x06, 0x68, 0x65,
	0x69, 0x67, 0x68, 0x74, 0x22, 0x28, 0x0a, 0x0a, 0x43, 0x6f, 0x6f, 0x72, 0x64, 0x69, 0x6e, 0x61,
	0x74, 0x65, 0x12, 0x0c, 0x0a, 0x01, 0x78, 0x18, 0x01, 0x20, 0x01, 0x28, 0x05, 0x52, 0x01, 0x78,
	0x12, 0x0c, 0x0a, 0x01, 0x79, 0x18, 0x02, 0x20, 0x01, 0x28, 0x05, 0x52, 0x01, 0x79, 0x22, 0x20,
	0x0a, 0x04, 0x54, 0x65, 0x78, 0x74, 0x12, 0x18, 0x0a, 0x07, 0x63, 0x6f, 0x6e, 0x74, 0x65, 0x6e,
	0x74, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x07, 0x63, 0x6f, 0x6e, 0x74, 0x65, 0x6e, 0x74,
	0x22, 0x17, 0x0a, 0x03, 0x55, 0x72, 0x6c, 0x12, 0x10, 0x0a, 0x03, 0x75, 0x72, 0x6c, 0x18, 0x01,
	0x20, 0x01, 0x28, 0x09, 0x52, 0x03, 0x75, 0x72, 0x6c, 0x22, 0x20, 0x0a, 0x0a, 0x53, 0x63, 0x72,
	0x65, 0x65, 0x6e, 0x73, 0x68, 0x6f, 0x74, 0x12, 0x12, 0x0a, 0x04, 0x64, 0x61, 0x74, 0x61, 0x18,
	0x01, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x04, 0x64, 0x61, 0x74, 0x61, 0x32, 0x98, 0x02, 0x0a, 0x0e,
	0x42, 0x72, 0x6f, 0x77, 0x73, 0x65, 0x72, 0x43, 0x6f, 0x6e, 0x74, 0x72, 0x6f, 0x6c, 0x12, 0x23,
	0x0a, 0x07, 0x4f, 0x70, 0x65, 0x6e, 0x54, 0x61, 0x62, 0x12, 0x09, 0x2e, 0x70, 0x62, 0x2e, 0x45,
	0x6d, 0x70, 0x74, 0x79, 0x1a, 0x0b, 0x2e, 0x70, 0x62, 0x2e, 0x4d, 0x65, 0x73, 0x73, 0x61, 0x67,
	0x65, 0x22, 0x00, 0x12, 0x2e, 0x0a, 0x0b, 0x53, 0x65, 0x74, 0x56, 0x69, 0x65, 0x77, 0x70, 0x6f,
	0x72, 0x74, 0x12, 0x10, 0x2e, 0x70, 0x62, 0x2e, 0x56, 0x69, 0x65, 0x77, 0x70, 0x6f, 0x72, 0x74,
	0x53, 0x69, 0x7a, 0x65, 0x1a, 0x0b, 0x2e, 0x70, 0x62, 0x2e, 0x4d, 0x65, 0x73, 0x73, 0x61, 0x67,
	0x65, 0x22, 0x00, 0x12, 0x2b, 0x0a, 0x0a, 0x43, 0x6c, 0x69, 0x63, 0x6b, 0x4d, 0x6f, 0x75, 0x73,
	0x65, 0x12, 0x0e, 0x2e, 0x70, 0x62, 0x2e, 0x43, 0x6f, 0x6f, 0x72, 0x64, 0x69, 0x6e, 0x61, 0x74,
	0x65, 0x1a, 0x0b, 0x2e, 0x70, 0x62, 0x2e, 0x4d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x22, 0x00,
	0x12, 0x2c, 0x0a, 0x11, 0x53, 0x65, 0x6e, 0x64, 0x4b, 0x65, 0x79, 0x62, 0x6f, 0x61, 0x72, 0x64,
	0x49, 0x6e, 0x70, 0x75, 0x74, 0x12, 0x08, 0x2e, 0x70, 0x62, 0x2e, 0x54, 0x65, 0x78, 0x74, 0x1a,
	0x0b, 0x2e, 0x70, 0x62, 0x2e, 0x4d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x22, 0x00, 0x12, 0x27,
	0x0a, 0x0d, 0x4e, 0x61, 0x76, 0x69, 0x67, 0x61, 0x74, 0x65, 0x54, 0x6f, 0x55, 0x72, 0x6c, 0x12,
	0x07, 0x2e, 0x70, 0x62, 0x2e, 0x55, 0x72, 0x6c, 0x1a, 0x0b, 0x2e, 0x70, 0x62, 0x2e, 0x4d, 0x65,
	0x73, 0x73, 0x61, 0x67, 0x65, 0x22, 0x00, 0x12, 0x2d, 0x0a, 0x0e, 0x54, 0x61, 0x6b, 0x65, 0x53,
	0x63, 0x72, 0x65, 0x65, 0x6e, 0x73, 0x68, 0x6f, 0x74, 0x12, 0x09, 0x2e, 0x70, 0x62, 0x2e, 0x45,
	0x6d, 0x70, 0x74, 0x79, 0x1a, 0x0e, 0x2e, 0x70, 0x62, 0x2e, 0x53, 0x63, 0x72, 0x65, 0x65, 0x6e,
	0x73, 0x68, 0x6f, 0x74, 0x22, 0x00, 0x42, 0x10, 0x5a, 0x0e, 0x74, 0x65, 0x72, 0x6d, 0x69, 0x75,
	0x6d, 0x2f, 0x73, 0x72, 0x63, 0x2f, 0x70, 0x62, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_bc_proto_rawDescOnce sync.Once
	file_bc_proto_rawDescData = file_bc_proto_rawDesc
)

func file_bc_proto_rawDescGZIP() []byte {
	file_bc_proto_rawDescOnce.Do(func() {
		file_bc_proto_rawDescData = protoimpl.X.CompressGZIP(file_bc_proto_rawDescData)
	})
	return file_bc_proto_rawDescData
}

var file_bc_proto_msgTypes = make([]protoimpl.MessageInfo, 7)
var file_bc_proto_goTypes = []any{
	(*Empty)(nil),        // 0: pb.Empty
	(*Message)(nil),      // 1: pb.Message
	(*ViewportSize)(nil), // 2: pb.ViewportSize
	(*Coordinate)(nil),   // 3: pb.Coordinate
	(*Text)(nil),         // 4: pb.Text
	(*Url)(nil),          // 5: pb.Url
	(*Screenshot)(nil),   // 6: pb.Screenshot
}
var file_bc_proto_depIdxs = []int32{
	0, // 0: pb.BrowserControl.OpenTab:input_type -> pb.Empty
	2, // 1: pb.BrowserControl.SetViewport:input_type -> pb.ViewportSize
	3, // 2: pb.BrowserControl.ClickMouse:input_type -> pb.Coordinate
	4, // 3: pb.BrowserControl.SendKeyboardInput:input_type -> pb.Text
	5, // 4: pb.BrowserControl.NavigateToUrl:input_type -> pb.Url
	0, // 5: pb.BrowserControl.TakeScreenshot:input_type -> pb.Empty
	1, // 6: pb.BrowserControl.OpenTab:output_type -> pb.Message
	1, // 7: pb.BrowserControl.SetViewport:output_type -> pb.Message
	1, // 8: pb.BrowserControl.ClickMouse:output_type -> pb.Message
	1, // 9: pb.BrowserControl.SendKeyboardInput:output_type -> pb.Message
	1, // 10: pb.BrowserControl.NavigateToUrl:output_type -> pb.Message
	6, // 11: pb.BrowserControl.TakeScreenshot:output_type -> pb.Screenshot
	6, // [6:12] is the sub-list for method output_type
	0, // [0:6] is the sub-list for method input_type
	0, // [0:0] is the sub-list for extension type_name
	0, // [0:0] is the sub-list for extension extendee
	0, // [0:0] is the sub-list for field type_name
}

func init() { file_bc_proto_init() }
func file_bc_proto_init() {
	if File_bc_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_bc_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   7,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_bc_proto_goTypes,
		DependencyIndexes: file_bc_proto_depIdxs,
		MessageInfos:      file_bc_proto_msgTypes,
	}.Build()
	File_bc_proto = out.File
	file_bc_proto_rawDesc = nil
	file_bc_proto_goTypes = nil
	file_bc_proto_depIdxs = nil
}
