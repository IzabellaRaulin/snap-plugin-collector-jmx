/*
Copyright 2016 Staples, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package jmx

import (
	"net/http"
	"io/ioutil"
	"errors"
	"encoding/json"
	"strings"
	"time"
        "bytes"
        "reflect"
        "log"
        "strconv"

	"github.com/intelsdi-x/snap/control/plugin"
	"github.com/intelsdi-x/snap/control/plugin/cpolicy"
	"github.com/intelsdi-x/snap/core"
	"github.com/intelsdi-x/snap/core/ctypes"
	) 

const (
	// Name of plugin
	Name = "jmx"
	// Version of plugin
	Version = 1
	// Type of plugin
	Type = plugin.CollectorPluginType

        HTTP_200_OK = 200
        HTTP_CONN_TIME_OUT = 3
)

var (
	ErrorJmxServerUrlCfg = errors.New("Failed to parse given connection url")
	ErrorConfigRead = errors.New("Config read Error")
	ErrorJmxMBeanCfg = errors.New("mbean Config read Error")
	ErrorJmxAppNameCfg = errors.New("App Name Config read Error")
        ErrorAttrNotFound = errors.New("Attribute Not Found Exeception")
        ErrorConnRefused = errors.New("Connection refused to host")
)    	 

// make sure that we actually satisify requierd interface
var _ plugin.CollectorPlugin = (*Jmx)(nil)

type Jmx struct {}	

type target struct {
    Url string `json:"url"`
}

//Post Qurey structure
type jPost struct {
   Type string `json:"type"`
   Mbean string `json:"mbean"`
   Attribute []string `json:"attribute,omitempty"`
   Target target `json:"target"`
}


//Ignore metric in given list
func checkIgnoreMetric(mkey string)(bool) {
    ignoreChildMetric := "ObjectName"
    ignoreMetric := ""
    ret := false
    if strings.EqualFold(ignoreChildMetric,"nil") == false {
	if strings.Contains(ignoreChildMetric,mkey) == true {
   	   ret = true
	}
    }

    if strings.EqualFold(ignoreMetric,"nil") == false {
	if strings.Contains(ignoreMetric,mkey) == true {
	   ret = true
	}
    }
    return ret
}

//Get Namespace based on snap requirment
func getNamespace(mkey string) (ns core.Namespace) {
    replacer := strings.NewReplacer(".", "-", ",", "-", " ", "-")
    rc := replacer.Replace(mkey)
    ss := strings.Split(rc, "/")
    ns = core.NewNamespace(ss...)
    return ns
}

//Flattern json struct 
func switchType(outMetric *[]plugin.MetricType, mval interface{}, ak string) {
    switch mtype := mval.(type) {
    case bool:
       ns := getNamespace(ak)
       tmp := plugin.MetricType{}
       tmp.Namespace_= ns
       if mval.(bool) == false {
	   tmp.Data_= 0 
       } else {
	   tmp.Data_= 1
       }
       tmp.Timestamp_= time.Now()
       *outMetric = append(*outMetric, tmp)
    case int, int64, float64, string:
       ns := getNamespace(ak)
       tmp := plugin.MetricType{}
       tmp.Namespace_= ns
       tmp.Data_=      mval
       tmp.Timestamp_= time.Now()
       *outMetric = append(*outMetric, tmp)
    case map[string]interface{}:
	parseMetrics(outMetric, mtype, ak)
//Removed 
//    case []interface{}:
//	parseArrMetrics(outMetric, mtype, ak)
    default:
	log.Println("In default missing type =", reflect.TypeOf(mval))
    }
    return
}

//Parser Array of Josn Metrics
func parseArrMetrics(outMetric *[]plugin.MetricType, inData []interface{}, parentKey string) {
    for mkey, mval := range inData {
        switchType(outMetric, mval, parentKey+"/"+strconv.Itoa(mkey))
    }
    return
}

//Parser Json Metrics
func parseMetrics(outMetric *[]plugin.MetricType, inData map[string]interface{}, pkey string)  {
    for mkey, mval := range inData {
        if checkIgnoreMetric(mkey) == true {
	   continue
	}
	ak := pkey + "/" + mkey 
	switchType(outMetric, mval, ak)
    }	
}

//Post Query and get responce
func postQuery(webserver string, jsonStr []byte)([]byte, error) {
   
    req, err := http.NewRequest("POST", webserver, bytes.NewBuffer(jsonStr))
    req.Header.Set("X-Custom-Header", "myvalue")
    req.Header.Set("Content-Type", "application/json")

    //log.Println("req =",req)

    timeout := time.Duration(HTTP_CONN_TIME_OUT * time.Second)

    client := &http.Client{
                Timeout: timeout,
              }
    resp, err := client.Do(req)
    //log.Println("resp =",resp,"err =", err)
    if err != nil {
       return nil, err 
    }

    defer resp.Body.Close()

    //log.Println("response Status:", resp.Status)
    //log.Println("response Headers:", resp.Header)
    body, err := ioutil.ReadAll(resp.Body)
    //log.Println("response Body:", string(body))

    if  strings.Contains(string(body),"AttributeNotFoundException") == true {
      return nil,ErrorAttrNotFound
    } else if strings.Contains(string(body),"Connection refused to host") == true {
      return nil,ErrorConnRefused
    }

    return body, err
}


//Get Metrics from given Jmx and Jolokia URL
func getMetrics(appname string, webserver string, mbean string, metrics []string)(mts []plugin.MetricType, err error) {

    arrWebSer := strings.Split(webserver,"|")
    //log.Println("arrWebSer =",arrWebSer,"len arrWebSer =",len(arrWebSer))

    //Loop for all URL given
    for i:=0; i < len(arrWebSer); i++ {
        /////var jp []jPost
        wsattr := strings.Split(arrWebSer[i],"+")  
        //log.Println("wasattr =", wsattr)

        arrMbean := strings.Split(mbean,"|")
        //log.Println("arrMbean =",arrMbean)

        //Loop for all requested mbean
        for j:= 0; j < len(arrMbean); j++ {
            mbattr := strings.Split(arrMbean[j],"^")
            //log.Println(mbattr)                         

            var arrmbattr []string
            var jpTmp jPost

            jpTmp.Type = mbattr[0]
            jpTmp.Mbean = mbattr[1]
            if len(mbattr) == 3 {
               arrmbattr = strings.Split(mbattr[2],"+")
               //log.Println("arrmbattr =", arrmbattr)
               for v := 0; v < len(arrmbattr); v++ {
                  jpTmp.Attribute = append(jpTmp.Attribute,arrmbattr[v])
               }
            }

            if len(wsattr) > 1 {
               jpTmp.Target.Url = wsattr[1]
            } 

            //marshal to json string
            jsonStr, err := json.Marshal(jpTmp)
            if err != nil {
                log.Println(err)
                continue
            }
            //log.Println("jsonStr =", string(jsonStr))

            //Post query
            jresp, err := postQuery(wsattr[0], jsonStr)
            if err != nil {
               log.Println(err)
               continue
            }

            //log.Println("jresp =",string(jresp))

            //Unmarshal to get Json responce
            jFmt := make(map[string]interface{})
            err = json.Unmarshal(jresp, &jFmt)
            if err != nil {
                log.Println(err)
                continue
            }

            //log.Println("jFmt =", jFmt)

            //Check status is 200OK
            status := jFmt["status"]
            if HTTP_200_OK == status.(int) {

               request := jFmt["request"]

               mb := request.(map[string]interface{})
               _, ok := mb["mbean"]
               //if responce miss mbean continue
               if !ok { 
                 continue
               }

               value := jFmt["value"]
               val := value.(map[string]interface{})

               pk :="staples" +"/" +"jmx" +"/" +appname + "/" + mb["mbean"].(string)

               //parser metrics
               parseMetrics(&mts, val, pk)
               
            } else {
              //log.Println("Http Responce status = ", jFmt) 
            }

        }//Loop for mbean

    }//Lopp for all URL

 //   log.Println("getMetrics mts =", mts,"err =", err)
    return mts,nil 
}


//CollectMetrics API definition
func (j *Jmx) CollectMetrics(inmts []plugin.MetricType) ( mts []plugin.MetricType, err error) {

    jmxAppNameCfg := inmts[0].Config().Table()["jmx_app_name"]
    jmxServerUrlCfg := inmts[0].Config().Table()["jmx_connection_url"]
    jmxMBeanCfg := inmts[0].Config().Table()["jmx_mbean_cfg"]


    if jmxAppNameCfg == nil || jmxServerUrlCfg == nil || jmxMBeanCfg == nil {
       return nil, ErrorConfigRead
    }

    jmxServer, ok := jmxServerUrlCfg.(ctypes.ConfigValueStr)
    if !ok {
       return nil, ErrorJmxServerUrlCfg
    }

    jmxMBean, ok := jmxMBeanCfg.(ctypes.ConfigValueStr)
    if !ok {
       return nil, ErrorJmxMBeanCfg
    }

    jmxAppName, ok := jmxAppNameCfg.(ctypes.ConfigValueStr)
    if !ok {
       return nil, ErrorJmxAppNameCfg
    }

    mts, err = getMetrics(jmxAppName.Value, jmxServer.Value,  jmxMBean.Value, []string{})

//    log.Println("GetMereicsTypes mts =", mts, "err =",err)

    return mts, err
}

//GetMetricTypes API definition
func (j *Jmx) GetMetricTypes(cfg plugin.ConfigType) (mts []plugin.MetricType, err error) {

        log.Println("Debug, Iza")
    jmxAppNameCfg := cfg.Table()["jmx_app_name"]
    jmxServerUrlCfg := cfg.Table()["jmx_connection_url"]
    jmxMBeanCfg := cfg.Table()["jmx_mbean_cfg"]


    if jmxAppNameCfg == nil || jmxServerUrlCfg == nil || jmxMBeanCfg == nil {
       return nil, ErrorConfigRead
    }

    jmxServer, ok := jmxServerUrlCfg.(ctypes.ConfigValueStr)
    if !ok {
       return nil, ErrorJmxServerUrlCfg
    }

    jmxMBean, ok := jmxMBeanCfg.(ctypes.ConfigValueStr)
    if !ok {
       return nil, ErrorJmxMBeanCfg
    }

    jmxAppName, ok := jmxAppNameCfg.(ctypes.ConfigValueStr)
    if !ok {
       return nil, ErrorJmxAppNameCfg
    }

    mts, err = getMetrics(jmxAppName.Value, jmxServer.Value,  jmxMBean.Value, []string{})

//    log.Println("GetMereicsTypes mts =", mts, "err =",err)

    return mts, err
}


//GetConfigPolicy API definition
func (j *Jmx) GetConfigPolicy() (*cpolicy.ConfigPolicy, error) {
    cfg := cpolicy.New()

    jmxAppName,_ := cpolicy.NewStringRule("jmx_app_name", true ,"jmx")

    jmxServerUrl,_ := cpolicy.NewStringRule("jmx_connection_url", true ,"http://localhost:8080/jolokia/+service:jmx:rmi:///jndi/rmi://localhost:9180/jmxrmi")

    jmxMBean,_ := cpolicy.NewStringRule("jmx_mbean_cfg", true ,"read,java.lang:type=Threading|read,java.lang:type=OperatingSystem")

    policy := cpolicy.NewPolicyNode()
    policy.Add(jmxAppName)
    policy.Add(jmxServerUrl)
    policy.Add(jmxMBean)

    cfg.Add([]string{"staples","jmx"},policy)

    return cfg, nil
}

//Meta API definition
func Meta() *plugin.PluginMeta {
    return plugin.NewPluginMeta(
	Name,
	Version,
	Type,
	[]string{plugin.SnapGOBContentType},
	[]string{plugin.SnapGOBContentType},
	plugin.Unsecure(true),
	plugin.RoutingStrategy(plugin.DefaultRouting),
	plugin.CacheTTL(1100*time.Millisecond),
    )
}
