// spot@public.aggre.deals.v3.api.pb

// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.6
// 	protoc        v6.31.1
// source: PublicAggreDealsV3Api.proto

package __

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
	unsafe "unsafe"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type PublicAggreDealsV3Api struct {
	state         protoimpl.MessageState       `protogen:"open.v1"`
	Deals         []*PublicAggreDealsV3ApiItem `protobuf:"bytes,1,rep,name=deals,proto3" json:"deals,omitempty"`
	EventType     string                       `protobuf:"bytes,2,opt,name=eventType,proto3" json:"eventType,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *PublicAggreDealsV3Api) Reset() {
	*x = PublicAggreDealsV3Api{}
	mi := &file_PublicAggreDealsV3Api_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *PublicAggreDealsV3Api) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*PublicAggreDealsV3Api) ProtoMessage() {}

func (x *PublicAggreDealsV3Api) ProtoReflect() protoreflect.Message {
	mi := &file_PublicAggreDealsV3Api_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use PublicAggreDealsV3Api.ProtoReflect.Descriptor instead.
func (*PublicAggreDealsV3Api) Descriptor() ([]byte, []int) {
	return file_PublicAggreDealsV3Api_proto_rawDescGZIP(), []int{0}
}

func (x *PublicAggreDealsV3Api) GetDeals() []*PublicAggreDealsV3ApiItem {
	if x != nil {
		return x.Deals
	}
	return nil
}

func (x *PublicAggreDealsV3Api) GetEventType() string {
	if x != nil {
		return x.EventType
	}
	return ""
}

type PublicAggreDealsV3ApiItem struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Price         string                 `protobuf:"bytes,1,opt,name=price,proto3" json:"price,omitempty"`
	Quantity      string                 `protobuf:"bytes,2,opt,name=quantity,proto3" json:"quantity,omitempty"`
	TradeType     int32                  `protobuf:"varint,3,opt,name=tradeType,proto3" json:"tradeType,omitempty"`
	Time          int64                  `protobuf:"varint,4,opt,name=time,proto3" json:"time,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *PublicAggreDealsV3ApiItem) Reset() {
	*x = PublicAggreDealsV3ApiItem{}
	mi := &file_PublicAggreDealsV3Api_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *PublicAggreDealsV3ApiItem) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*PublicAggreDealsV3ApiItem) ProtoMessage() {}

func (x *PublicAggreDealsV3ApiItem) ProtoReflect() protoreflect.Message {
	mi := &file_PublicAggreDealsV3Api_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use PublicAggreDealsV3ApiItem.ProtoReflect.Descriptor instead.
func (*PublicAggreDealsV3ApiItem) Descriptor() ([]byte, []int) {
	return file_PublicAggreDealsV3Api_proto_rawDescGZIP(), []int{1}
}

func (x *PublicAggreDealsV3ApiItem) GetPrice() string {
	if x != nil {
		return x.Price
	}
	return ""
}

func (x *PublicAggreDealsV3ApiItem) GetQuantity() string {
	if x != nil {
		return x.Quantity
	}
	return ""
}

func (x *PublicAggreDealsV3ApiItem) GetTradeType() int32 {
	if x != nil {
		return x.TradeType
	}
	return 0
}

func (x *PublicAggreDealsV3ApiItem) GetTime() int64 {
	if x != nil {
		return x.Time
	}
	return 0
}

var File_PublicAggreDealsV3Api_proto protoreflect.FileDescriptor

const file_PublicAggreDealsV3Api_proto_rawDesc = "" +
	"\n" +
	"\x1bPublicAggreDealsV3Api.proto\"g\n" +
	"\x15PublicAggreDealsV3Api\x120\n" +
	"\x05deals\x18\x01 \x03(\v2\x1a.PublicAggreDealsV3ApiItemR\x05deals\x12\x1c\n" +
	"\teventType\x18\x02 \x01(\tR\teventType\"\x7f\n" +
	"\x19PublicAggreDealsV3ApiItem\x12\x14\n" +
	"\x05price\x18\x01 \x01(\tR\x05price\x12\x1a\n" +
	"\bquantity\x18\x02 \x01(\tR\bquantity\x12\x1c\n" +
	"\ttradeType\x18\x03 \x01(\x05R\ttradeType\x12\x12\n" +
	"\x04time\x18\x04 \x01(\x03R\x04timeB>\n" +
	"\x1ccom.mxc.push.common.protobufB\x1aPublicAggreDealsV3ApiProtoH\x01P\x01b\x06proto3"

var (
	file_PublicAggreDealsV3Api_proto_rawDescOnce sync.Once
	file_PublicAggreDealsV3Api_proto_rawDescData []byte
)

func file_PublicAggreDealsV3Api_proto_rawDescGZIP() []byte {
	file_PublicAggreDealsV3Api_proto_rawDescOnce.Do(func() {
		file_PublicAggreDealsV3Api_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_PublicAggreDealsV3Api_proto_rawDesc), len(file_PublicAggreDealsV3Api_proto_rawDesc)))
	})
	return file_PublicAggreDealsV3Api_proto_rawDescData
}

var file_PublicAggreDealsV3Api_proto_msgTypes = make([]protoimpl.MessageInfo, 2)
var file_PublicAggreDealsV3Api_proto_goTypes = []any{
	(*PublicAggreDealsV3Api)(nil),     // 0: PublicAggreDealsV3Api
	(*PublicAggreDealsV3ApiItem)(nil), // 1: PublicAggreDealsV3ApiItem
}
var file_PublicAggreDealsV3Api_proto_depIdxs = []int32{
	1, // 0: PublicAggreDealsV3Api.deals:type_name -> PublicAggreDealsV3ApiItem
	1, // [1:1] is the sub-list for method output_type
	1, // [1:1] is the sub-list for method input_type
	1, // [1:1] is the sub-list for extension type_name
	1, // [1:1] is the sub-list for extension extendee
	0, // [0:1] is the sub-list for field type_name
}

func init() { file_PublicAggreDealsV3Api_proto_init() }
func file_PublicAggreDealsV3Api_proto_init() {
	if File_PublicAggreDealsV3Api_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_PublicAggreDealsV3Api_proto_rawDesc), len(file_PublicAggreDealsV3Api_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   2,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_PublicAggreDealsV3Api_proto_goTypes,
		DependencyIndexes: file_PublicAggreDealsV3Api_proto_depIdxs,
		MessageInfos:      file_PublicAggreDealsV3Api_proto_msgTypes,
	}.Build()
	File_PublicAggreDealsV3Api_proto = out.File
	file_PublicAggreDealsV3Api_proto_goTypes = nil
	file_PublicAggreDealsV3Api_proto_depIdxs = nil
}
