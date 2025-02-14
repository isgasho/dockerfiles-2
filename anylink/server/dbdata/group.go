package dbdata

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/bjdgyc/anylink/base"
)

const (
	Allow = "allow"
	Deny  = "deny"
	All   = "all"
)

type GroupLinkAcl struct {
	// 自上而下匹配 默认 allow * *
	Action string     `json:"action"` // allow、deny
	Val    string     `json:"val"`
	Port   uint16     `json:"port"`
	IpNet  *net.IPNet `json:"ip_net"`
	Note   string     `json:"note"`
}

type ValData struct {
	Val    string `json:"val"`
	IpMask string `json:"ip_mask"`
	Note   string `json:"note"`
}

// type Group struct {
// 	Id               int                    `json:"id" xorm:"pk autoincr not null"`
// 	Name             string                 `json:"name" xorm:"varchar(60) not null unique"`
// 	Note             string                 `json:"note" xorm:"varchar(255)"`
// 	AllowLan         bool                   `json:"allow_lan" xorm:"Bool"`
// 	ClientDns        []ValData              `json:"client_dns" xorm:"Text"`
// 	RouteInclude     []ValData              `json:"route_include" xorm:"Text"`
// 	RouteExclude     []ValData              `json:"route_exclude" xorm:"Text"`
// 	DsExcludeDomains string                 `json:"ds_exclude_domains" xorm:"Text"`
// 	DsIncludeDomains string                 `json:"ds_include_domains" xorm:"Text"`
// 	LinkAcl          []GroupLinkAcl         `json:"link_acl" xorm:"Text"`
// 	Bandwidth        int                    `json:"bandwidth" xorm:"Int"`                           // 带宽限制
// 	Auth             map[string]interface{} `json:"auth" xorm:"not null default '{}' varchar(255)"` // 认证方式
// 	Status           int8                   `json:"status" xorm:"Int"`                              // 1正常
// 	CreatedAt        time.Time              `json:"created_at" xorm:"DateTime created"`
// 	UpdatedAt        time.Time              `json:"updated_at" xorm:"DateTime updated"`
// }

func GetGroupNames() []string {
	var datas []Group
	err := Find(&datas, 0, 0)
	if err != nil {
		base.Error(err)
		return nil
	}
	var names []string
	for _, v := range datas {
		names = append(names, v.Name)
	}
	return names
}

func SetGroup(g *Group) error {
	var err error
	if g.Name == "" {
		return errors.New("用户组名错误")
	}

	// 判断数据
	routeInclude := []ValData{}
	for _, v := range g.RouteInclude {
		if v.Val != "" {
			if v.Val == All {
				routeInclude = append(routeInclude, v)
				continue
			}

			ipMask, _, err := parseIpNet(v.Val)
			if err != nil {
				return errors.New("RouteInclude 错误" + err.Error())
			}

			v.IpMask = ipMask
			routeInclude = append(routeInclude, v)
		}
	}
	g.RouteInclude = routeInclude
	routeExclude := []ValData{}
	for _, v := range g.RouteExclude {
		if v.Val != "" {
			ipMask, _, err := parseIpNet(v.Val)
			if err != nil {
				return errors.New("RouteExclude 错误" + err.Error())
			}
			v.IpMask = ipMask
			routeExclude = append(routeExclude, v)
		}
	}
	g.RouteExclude = routeExclude
	// 转换数据
	linkAcl := []GroupLinkAcl{}
	for _, v := range g.LinkAcl {
		if v.Val != "" {
			_, ipNet, err := parseIpNet(v.Val)
			if err != nil {
				return errors.New("GroupLinkAcl 错误" + err.Error())
			}
			v.IpNet = ipNet
			linkAcl = append(linkAcl, v)
		}
	}
	g.LinkAcl = linkAcl

	// DNS 判断
	clientDns := []ValData{}
	for _, v := range g.ClientDns {
		if v.Val != "" {
			ip := net.ParseIP(v.Val)
			if ip.String() != v.Val {
				return errors.New("DNS IP 错误")
			}
			clientDns = append(clientDns, v)
		}
	}
	if len(routeInclude) == 0 || (len(routeInclude) == 1 && routeInclude[0].Val == "all") {
		if len(clientDns) == 0 {
			return errors.New("默认路由，必须设置一个DNS")
		}
	}
	g.ClientDns = clientDns
	// 域名拆分隧道，不能同时填写
	g.DsIncludeDomains = strings.TrimSpace(g.DsIncludeDomains)
	g.DsExcludeDomains = strings.TrimSpace(g.DsExcludeDomains)
	if g.DsIncludeDomains != "" && g.DsExcludeDomains != "" {
		return errors.New("包含/排除域名不能同时填写")
	}
	// 校验包含域名的格式
	err = CheckDomainNames(g.DsIncludeDomains)
	if err != nil {
		return errors.New("包含域名有误：" + err.Error())
	}
	// 校验排除域名的格式
	err = CheckDomainNames(g.DsExcludeDomains)
	if err != nil {
		return errors.New("排除域名有误：" + err.Error())
	}
	// 处理登入方式的逻辑
	defAuth := map[string]interface{}{
		"type": "local",
	}
	if len(g.Auth) == 0 {
		g.Auth = defAuth
	}
	authType := g.Auth["type"].(string)
	if authType == "local" {
		g.Auth = defAuth
	} else {
		_, ok := authRegistry[authType]
		if !ok {
			return errors.New("未知的认证方式: " + authType)
		}
		auth := makeInstance(authType).(IUserAuth)
		err = auth.checkData(g.Auth)
		if err != nil {
			return err
		}
	}

	g.UpdatedAt = time.Now()
	if g.Id > 0 {
		err = Set(g)
	} else {
		err = Add(g)
	}

	return err
}

func parseIpNet(s string) (string, *net.IPNet, error) {
	ip, ipNet, err := net.ParseCIDR(s)
	if err != nil {
		return "", nil, err
	}

	mask := net.IP(ipNet.Mask)
	ipMask := fmt.Sprintf("%s/%s", ip, mask)

	return ipMask, ipNet, nil
}

func CheckDomainNames(domains string) error {
	if domains == "" {
		return nil
	}
	str_slice := strings.Split(domains, ",")
	for _, val := range str_slice {
		if val == "" {
			return errors.New(val + " 请以逗号分隔域名")
		}
		if !ValidateDomainName(val) {
			return errors.New(val + " 域名有误")
		}
	}
	return nil
}

func ValidateDomainName(domain string) bool {
	RegExp := regexp.MustCompile(`^([a-zA-Z0-9][-a-zA-Z0-9]{0,62}\.)+[A-Za-z]{2,18}$`)
	return RegExp.MatchString(domain)
}
