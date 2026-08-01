package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

const base = "http://localhost:5230"

type loginResp struct {
	Access string `json:"access"`
}

type articleResp struct {
	ID int64 `json:"id"`
}

func post(path, token string, body any) ([]byte, int) {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest("POST", base+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR %s: %v\n", path, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode
}

func login(username string) string {
	data, code := post("/api/auth/login", "", map[string]string{"username": username, "password": "Pass1234"})
	if code != 200 {
		fmt.Fprintf(os.Stderr, "login %s failed: %d %s\n", username, code, data)
		os.Exit(1)
	}
	var r loginResp
	json.Unmarshal(data, &r)
	return r.Access
}

func createArticle(token, title, content string, deptID int64) int64 {
	body := map[string]any{"title": title, "content": content, "department_id": deptID, "allow_reference": true}
	data, code := post("/api/staff/wiki/articles", token, body)
	if code != 201 && code != 200 {
		fmt.Fprintf(os.Stderr, "create article failed: %d %s\n", code, data)
		os.Exit(1)
	}
	var r articleResp
	json.Unmarshal(data, &r)
	return r.ID
}

func submitApprove(token string, ids []int64) {
	for _, id := range ids {
		if _, code := post(fmt.Sprintf("/api/staff/wiki/articles/%d/submit", id), token, nil); code != 200 {
			fmt.Fprintf(os.Stderr, "submit %d failed: %d\n", id, code)
			os.Exit(1)
		}
		fmt.Printf("Submitted: %d\n", id)
	}
	for _, id := range ids {
		if _, code := post(fmt.Sprintf("/api/staff/wiki/articles/%d/approve", id), token, nil); code != 200 {
			fmt.Fprintf(os.Stderr, "approve %d failed: %d\n", id, code)
			os.Exit(1)
		}
		fmt.Printf("Approved: %d\n", id)
	}
}

func main() {
	token1 := login("admin1")
	fmt.Println("admin1 logged in (dept 4 心内科)")

	token2 := login("admin2")
	fmt.Println("admin2 logged in (dept 423 内分泌科)")

	art1 := `# 高血压患者日常注意事项

## 一、血压监测
高血压患者应每天定时测量血压，建议早晨起床后和晚上睡前各测量一次。测量前应静坐5分钟，避免运动、饮酒或吸烟后立即测量。正常血压应控制在140/90mmHg以下，理想血压为120/80mmHg。

## 二、饮食管理
高血压患者饮食应以低盐、低脂、低胆固醇为主。每日食盐摄入量不超过6克，避免腌制食品、加工肉类。建议多食用新鲜蔬菜水果，如芹菜、菠菜、香蕉等富含钾的食物有助于降压。减少饱和脂肪摄入，选择橄榄油等健康油脂。

## 三、运动建议
适当的有氧运动有助于降低血压。建议每周进行150分钟以上的中等强度有氧运动，如快走、游泳、骑自行车等。运动时应避免剧烈运动和憋气动作，运动前做好热身。血压超过180/110mmHg时应暂停运动。

## 四、用药注意事项
高血压患者必须遵医嘱规律服药，不可自行停药或减量。常用降压药包括钙通道阻滞剂（如氨氯地平）、ACEI类（如依那普利）、ARB类（如缬沙坦）等。服药期间如出现头晕、乏力等不适应及时就医。

## 五、生活方式调整
戒烟限酒是高血压管理的重要环节。吸烟会损伤血管内皮，加重动脉硬化。饮酒量应严格控制，男性每日酒精摄入不超过25克。保持充足睡眠，每晚7-8小时。学会减压，避免情绪激动，可通过冥想、深呼吸等方式放松身心。`

	art2 := `# 冠心病预防与康复指导

## 一、冠心病基础知识
冠状动脉粥样硬化性心脏病（冠心病）是由于冠状动脉狭窄或阻塞导致心肌缺血缺氧的疾病。主要危险因素包括高血压、高血脂、糖尿病、吸烟、肥胖和家族史。典型症状为胸骨后压榨性疼痛，可放射至左肩、左臂。

## 二、一级预防
冠心病一级预防的关键是控制危险因素。血脂管理目标为LDL-C低于2.6mmol/L，高危患者应低于1.8mmol/L。他汀类药物是降脂治疗的基石。血压控制在130/80mmHg以下。糖尿病患者糖化血红蛋白应控制在7%以下。

## 三、胸痛急性发作处理
当出现胸痛症状时，应立即停止活动，就地休息。舌下含服硝酸甘油1片，5分钟后如未缓解可再含1片，最多3片。同时拨打120急救电话。如怀疑急性心肌梗死，应嚼服阿司匹林300mg。切勿自行驾车前往医院。

## 四、心脏康复
冠心病患者出院后应尽早开始心脏康复。康复分为三期：院内期、恢复期和维持期。运动康复应在专业指导下进行，从低强度开始逐步增加。运动时心率不超过最大心率的70%。定期复查心电图、超声心动图和血脂指标。

## 五、心理调适
冠心病患者常伴有焦虑和抑郁情绪，这会影响康复进程。建议家属给予充分支持和理解。必要时可寻求心理咨询帮助。保持乐观心态，避免过度紧张和恐惧。参加病友交流活动有助于增强信心。`

	art3 := `# 糖尿病患者饮食管理指南

## 一、饮食原则
糖尿病饮食管理的核心是控制总热量摄入，保持营养均衡。每日总热量根据体重和活动量计算，一般每公斤体重25-30千卡。三大营养素比例为碳水化合物50-60%、蛋白质15-20%、脂肪25-30%。定时定量进餐，避免暴饮暴食。

## 二、碳水化合物选择
糖尿病患者应优先选择低升糖指数（GI）的食物。推荐全谷物如燕麦、荞麦、糙米，替代精白米面。避免含糖饮料、甜点、蜂蜜等高糖食品。水果应在血糖控制稳定时适量食用，选择低GI水果如苹果、柚子、草莓，每次不超过200克，放在两餐之间食用。

## 三、蛋白质与脂肪
优质蛋白质来源包括鱼、禽、蛋、瘦肉和豆制品。每日蛋白质摄入量约每公斤体重1克。合并肾病的患者应限制蛋白质至每公斤0.6-0.8克。脂肪摄入以不饱和脂肪酸为主，减少动物油脂和反式脂肪。每周食用2-3次深海鱼有助于心血管保护。

## 四、血糖监测与饮食调整
建议糖尿病患者进行自我血糖监测，包括空腹血糖和餐后2小时血糖。空腹血糖目标为4.4-7.0mmol/L，餐后2小时血糖低于10.0mmol/L。根据血糖波动情况及时调整饮食方案。记录饮食日记有助于发现影响血糖的食物和习惯。

## 五、特殊注意事项
糖尿病患者应限制饮酒，空腹饮酒可能导致低血糖。外出就餐时注意控制油盐和主食量。合并高血压的糖尿病患者应同时限制钠盐摄入。定期复查糖化血红蛋白（每3个月一次），目标控制在7%以下。`

	art4 := `# 甲状腺功能异常科普

## 一、甲状腺基础知识
甲状腺是人体最大的内分泌腺，位于颈部前方，分泌甲状腺激素（T3、T4）调节新陈代谢。甲状腺功能异常主要包括甲状腺功能亢进（甲亢）和甲状腺功能减退（甲减）。甲状腺疾病在女性中更为常见，男女比例约为1:4-6。

## 二、甲亢的症状与治疗
甲亢的典型症状包括心悸、手抖、多汗、体重下降、焦虑易怒、眼球突出等。实验室检查表现为TSH降低、FT3和FT4升高。治疗方法包括抗甲状腺药物（甲巯咪唑、丙硫氧嘧啶）、放射性碘131治疗和手术治疗。药物治疗疗程通常为1.5-2年，不可擅自停药。

## 三、甲减的症状与治疗
甲减表现为乏力、怕冷、体重增加、便秘、皮肤干燥、记忆力减退等。实验室检查表现为TSH升高、FT4降低。最常见的病因是桥本甲状腺炎。治疗以左甲状腺素（优甲乐）替代治疗为主，需终身服药。服药应在早餐前30-60分钟空腹服用，避免与钙剂、铁剂同服。

## 四、甲状腺结节
甲状腺结节非常常见，超声检查发现率可达20-70%。绝大多数结节为良性，无需特殊治疗。TI-RADS分级4级以上的结节建议进行细针穿刺活检。结节患者应定期复查甲状腺超声，一般每6-12个月一次。避免过度摄入碘，但也不必完全忌碘。

## 五、日常注意事项
甲状腺疾病患者应保持规律作息，避免过度疲劳和精神压力。甲亢患者应忌碘饮食，避免海带、紫菜等高碘食物。甲减患者适量摄入碘即可。定期复查甲状腺功能，调整药物剂量。备孕和妊娠期女性应特别关注甲状腺功能，TSH目标值不同于一般人群。`

	fmt.Println("=== Creating dept 4 articles (admin1) ===")
	id1 := createArticle(token1, "高血压患者日常注意事项", art1, 4)
	fmt.Printf("Created: [%d]\n", id1)
	id2 := createArticle(token1, "冠心病预防与康复指导", art2, 4)
	fmt.Printf("Created: [%d]\n", id2)
	submitApprove(token1, []int64{id1, id2})

	fmt.Println("=== Creating dept 423 articles (admin2) ===")
	id3 := createArticle(token2, "糖尿病患者饮食管理指南", art3, 423)
	fmt.Printf("Created: [%d]\n", id3)
	id4 := createArticle(token2, "甲状腺功能异常科普", art4, 423)
	fmt.Printf("Created: [%d]\n", id4)
	submitApprove(token2, []int64{id3, id4})

	fmt.Printf("Done! Articles: %d, %d (心内科), %d, %d (内分泌科)\n", id1, id2, id3, id4)
}
