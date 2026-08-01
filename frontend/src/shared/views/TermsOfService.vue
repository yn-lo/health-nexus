<script setup lang="ts">
import { useRouter } from 'vue-router'
import { AppHeader } from '@/shared/components'

const router = useRouter()

function goBack() {
 if (window.history.length > 1) {
 router.back()
 } else {
 router.push('/')
 }
}
</script>

<template>
 <div class="terms-page">
 <AppHeader title="用户协议" @back="goBack" />
 <div class="terms-body px-[var(--spacer-20)] py-[var(--spacer-16)]">
 <p class="text-[var(--body-sm-font-size)] text-text-tertiary mb-[var(--spacer-16)]">
 更新日期：2026 年 7 月 30 日&emsp;生效日期：2026 年 7 月 30 日
 </p>

 <section class="terms-section">
 <h2>一、服务说明</h2>
 <p>
 健康枢纽（Health Nexus）是一个基于人工智能技术的医院健康宣教平台（以下简称"本平台"），
 旨在通过 RAG（检索增强生成）技术为患者提供 7×24 小时智能健康问答服务，
 帮助患者获取可溯源的健康指导，减轻医护人员重复宣教负担。
 </p>
 <p>
 本平台提供的健康信息仅供参考，<strong>不能替代专业医疗诊断、治疗或医嘱</strong>。
 如您有紧急医疗需求，请立即前往医院就诊或拨打 120 急救电话。
 </p>
 </section>

 <section class="terms-section">
 <h2>二、账号注册与安全</h2>
 <ol>
 <li>您注册时应提供真实、准确的个人信息，包括用户名、手机号码等。</li>
 <li>您应对账号和密码的安全性负责，不得将账号转让、出借或与他人共享。</li>
 <li>如发现账号被盗用或存在安全漏洞，您应立即联系本平台管理员。</li>
 <li>本平台有权对存在异常行为的账号采取暂停服务、封禁等措施。</li>
 </ol>
 </section>

 <section class="terms-section">
 <h2>三、用户行为规范</h2>
 <p>您在使用本平台时，不得从事以下行为：</p>
 <ol>
 <li>发布违法、淫秽、暴力、恐怖、侮辱、诽谤等违法违规内容。</li>
 <li>利用本平台进行垃圾广告、恶意营销、诈骗等活动。</li>
 <li>通过程序或脚本自动访问、抓取本平台数据，或对系统进行反向工程。</li>
 <li>干扰、破坏本平台正常运行，或利用漏洞进行非法操作。</li>
 <li>冒用他人身份或虚构身份信息进行注册和使用。</li>
 <li>其他违反法律法规、社会公序良俗的行为。</li>
 </ol>
 </section>

 <section class="terms-section">
 <h2>四、健康信息免责声明</h2>
 <ol>
 <li>
 <strong>本平台为辅助性健康信息工具，非医疗器械。</strong>
 AI 生成的健康建议基于公开医学文献和医院知识库，
 仅供参考，不构成医疗建议、诊断或治疗方案。
 </li>
 <li>
 您在做出任何医疗决策前，<strong>务必咨询专业医护人员</strong>。
 因信赖本平台信息而延误治疗或产生不良后果的，本平台不承担法律责任。
 </li>
 <li>
 本平台会标注信息来源（如参考文章、知识库条目），
 但不对第三方医学文献的准确性、完整性做任何保证。
 </li>
 <li>
 如您处于紧急医疗状态（如剧烈胸痛、呼吸困难、大出血等），
 请立即拨打 120 或前往就近急诊科，勿依赖本平台获取紧急指导。
 </li>
 </ol>
 </section>

 <section class="terms-section">
 <h2>五、知识产权</h2>
 <ol>
 <li>本平台的软件、界面设计、商标、Logo 等知识产权归本平台所有。</li>
 <li>您通过本平台输入的文字、上传的资料，其知识产权仍归您所有。
 但您授予本平台为提供服务所必需的、非独占的使用权。</li>
 <li>未经书面许可，任何人不得复制、修改、传播本平台的内容和代码。</li>
 <li>本平台引用的医学文献、指南等内容的著作权归原作者所有。</li>
 </ol>
 </section>

 <section class="terms-section">
 <h2>六、服务变更与终止</h2>
 <ol>
 <li>本平台有权根据业务需要，对服务内容进行调整、升级或终止，并提前通知用户。</li>
 <li>如用户违反本协议，本平台有权终止其账号使用权，且不退还任何费用。</li>
 <li>用户可随时申请注销账号，注销后其个人信息将按隐私政策处理。</li>
 </ol>
 </section>

 <section class="terms-section">
 <h2>七、责任限制</h2>
 <p>
 在法律允许的最大范围内，本平台不对因使用服务而产生的间接损失、数据丢失、
 业务中断等承担责任，即使本平台已被告知此类损害的可能性。
 </p>
 </section>

 <section class="terms-section">
 <h2>八、协议修改</h2>
 <p>
 本平台有权根据法律法规变化或服务调整修改本协议。修改后的协议将在平台上公布，
 并于公布之日起生效。如您继续使用本平台服务，视为同意修改后的协议。
 </p>
 </section>

 <section class="terms-section">
 <h2>九、适用法律与争议解决</h2>
 <ol>
 <li>本协议的订立、执行和解释均适用中华人民共和国法律。</li>
 <li>因本协议产生的争议，双方应友好协商解决；协商不成的，
 任何一方可向本平台所在地有管辖权的人民法院提起诉讼。</li>
 </ol>
 </section>

 <section class="terms-section">
 <h2>十、联系方式</h2>
 <p>如您对本协议有任何疑问，请联系：</p>
 <p>邮箱：support@health-nexus.example.com</p>
 </section>
 </div>
 </div>
</template>

<style scoped ponytail:allow-scoped-css 法律文档页面专用排版>
.terms-page {
 background: var(--bg-base-default);
 min-height: 100dvh;
 display: flex;
 flex-direction: column;
}

.terms-body {
 flex: 1;
 overflow-y: auto;
 padding-bottom: var(--spacer-32);
}

.terms-section {
 margin-bottom: var(--spacer-20);
}

.terms-section h2 {
 font-family: var(--font-family-heading);
 font-size: var(--heading-sm-font-size);
 line-height: var(--heading-sm-line-height);
 font-weight: 700;
 color: var(--text-default);
 margin-bottom: var(--spacer-8);
}

.terms-section p {
 font-size: var(--body-md-font-size);
 line-height: var(--body-md-line-height);
 color: var(--text-secondary);
 margin-bottom: var(--spacer-8);
}

.terms-section ol {
 list-style: decimal;
 padding-left: var(--spacer-16);
 margin-bottom: var(--spacer-8);
}

.terms-section ol li {
 font-size: var(--body-md-font-size);
 line-height: var(--body-md-line-height);
 color: var(--text-secondary);
 margin-bottom: var(--spacer-4);
}

.terms-section strong {
 color: var(--text-default);
}
</style>
