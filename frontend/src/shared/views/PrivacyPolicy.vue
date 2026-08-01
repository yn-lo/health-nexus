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
 <div class="privacy-page">
 <AppHeader title="隐私政策" @back="goBack" />
 <div class="privacy-body px-[var(--spacer-20)] py-[var(--spacer-16)]">
 <p class="text-[var(--body-sm-font-size)] text-text-tertiary mb-[var(--spacer-16)]">
 更新日期：2026 年 7 月 30 日&emsp;生效日期：2026 年 7 月 30 日
 </p>

 <p class="privacy-intro">
 健康枢纽（Health Nexus）（以下简称"本平台"或"我们"）深知个人信息对您的重要性，
 我们将严格按照《中华人民共和国个人信息保护法》《中华人民共和国数据安全法》
 《中华人民共和国网络安全法》等法律法规的要求，
 采取相应的安全保护措施，尽力保护您的个人信息安全可控。
 本隐私政策旨在向您说明我们如何收集、使用、存储和保护您的个人信息。
 </p>

 <section class="privacy-section">
 <h2>一、我们收集的信息</h2>

 <h3>1.1 您主动提供的信息</h3>
 <ul>
 <li><strong>账号信息：</strong>注册时您提供的用户名、手机号码、密码（加密存储）。</li>
 <li><strong>健康问答内容：</strong>您在对话中输入的健康问题、症状描述等文字信息。</li>
 <li><strong>反馈与评价：</strong>您提交的意见反馈、服务评价等内容。</li>
 </ul>

 <h3>1.2 自动收集的信息</h3>
 <ul>
 <li><strong>设备信息：</strong>设备型号、操作系统版本、浏览器类型等。</li>
 <li><strong>日志信息：</strong>访问时间、页面浏览记录、IP 地址等。</li>
 <li><strong>使用数据：</strong>对话记录（脱敏后用于服务优化和模型训练，不含个人身份信息）。</li>
 </ul>
 </section>

 <section class="privacy-section">
 <h2>二、信息的使用</h2>
 <p>我们收集的信息将用于以下目的：</p>
 <ol>
 <li>提供、维护和改进 AI 健康问答服务；</li>
 <li>分析使用趋势以优化 RAG 知识库的召回准确率；</li>
 <li>响应用户的咨询、投诉和反馈；</li>
 <li>检测和防范安全威胁、欺诈和其他恶意行为；</li>
 <li>遵守法律法规的要求。</li>
 </ol>
 <p>
 我们不会使用您的个人信息进行自动化决策，也不会基于健康数据对您进行用户画像分析。
 </p>
 </section>

 <section class="privacy-section">
 <h2>三、信息的存储</h2>
 <ol>
 <li><strong>存储地点：</strong>您的个人信息存储于中华人民共和国境内。</li>
 <li><strong>存储期限：</strong>账号有效期内持续存储；账号注销后，
 我们将在 30 个工作日内删除或匿名化处理您的个人信息，法律法规另有规定的除外。</li>
 <li><strong>安全措施：</strong>我们采用加密传输（TLS）、数据加密存储、
 访问权限控制、日志审计等技术手段保护您的信息安全。</li>
 </ol>
 </section>

 <section class="privacy-section">
 <h2>四、信息的共享与披露</h2>
 <p>我们承诺不会出售您的个人信息。仅在以下情况下可能共享您的信息：</p>
 <ol>
 <li><strong>获得您的明确同意；</strong></li>
 <li><strong>与医院合作方共享：</strong>
 当您的健康问题需要专业医疗人员介入时，经您同意后可能将匿名化的问题摘要共享给医院科室；</li>
 <li><strong>法律法规要求：</strong>根据法律法规、法院命令或政府机关的要求提供信息；</li>
 <li><strong>合并或收购：</strong>如发生业务重组，我们将要求新的实体继续遵守本隐私政策。</li>
 </ol>
 <p>除上述情况外，我们不会将您的健康对话内容与任何第三方共享。</p>
 </section>

 <section class="privacy-section">
 <h2>五、您的权利</h2>
 <p>根据相关法律法规，您享有以下权利：</p>
 <ol>
 <li><strong>查询权：</strong>您有权查询您的个人信息及处理情况。</li>
 <li><strong>更正权：</strong>您发现信息有误时，有权要求更正。</li>
 <li><strong>删除权：</strong>您可以申请注销账号，我们将删除或匿名化您的个人信息。</li>
 <li><strong>撤回同意权：</strong>您可以撤回对收集和使用个人信息的同意。</li>
 <li><strong>投诉权：</strong>您有权向监管部门投诉。</li>
 </ol>
 <p>行使上述权利，请通过本政策末尾的联系方式与我们联系。</p>
 </section>

 <section class="privacy-section">
 <h2>六、Cookie 和类似技术</h2>
 <p>
 本平台使用必要的 Cookie 和本地存储（localStorage）来维持会话状态、
 记住您的偏好设置以及保障服务安全。我们不会使用 Cookie 进行行为跟踪或广告投放。
 您可以通过浏览器设置管理或清除 Cookie，但这可能影响部分功能的正常使用。
 </p>
 </section>

 <section class="privacy-section">
 <h2>七、未成年人保护</h2>
 <p>
 本平台主要面向成年用户。如果您是未满 14 周岁的未成年人，
 请在监护人同意和陪同下使用本平台。我们不会故意收集未成年人的个人信息。
 如发现误收集，我们将立即删除。
 </p>
 </section>

 <section class="privacy-section">
 <h2>八、政策更新</h2>
 <p>
 我们可能根据法律法规或业务调整更新本隐私政策。更新后将在平台上公布，
 并于公布之日起生效。重大变更将以弹窗或消息通知的方式提醒您。
 </p>
 </section>

 <section class="privacy-section">
 <h2>九、联系方式</h2>
 <p>如您对本隐私政策有任何疑问、意见或投诉，请联系：</p>
 <ul>
 <li>数据保护负责人邮箱：dpo@health-nexus.example.com</li>
 <li>客服邮箱：support@health-nexus.example.com</li>
 </ul>
 </section>
 </div>
 </div>
</template>

<style scoped ponytail:allow-scoped-css 法律文档页面专用排版>
.privacy-page {
 background: var(--bg-base-default);
 min-height: 100dvh;
 display: flex;
 flex-direction: column;
}

.privacy-body {
 flex: 1;
 overflow-y: auto;
 padding-bottom: var(--spacer-32);
}

.privacy-intro {
 font-size: var(--body-md-font-size);
 line-height: var(--body-md-line-height);
 color: var(--text-secondary);
 margin-bottom: var(--spacer-20);
}

.privacy-section {
 margin-bottom: var(--spacer-20);
}

.privacy-section h2 {
 font-family: var(--font-family-heading);
 font-size: var(--heading-sm-font-size);
 line-height: var(--heading-sm-line-height);
 font-weight: 700;
 color: var(--text-default);
 margin-bottom: var(--spacer-8);
}

.privacy-section h3 {
 font-size: var(--body-base-font-size);
 line-height: var(--body-base-line-height);
 font-weight: 600;
 color: var(--text-default);
 margin-bottom: var(--spacer-4);
 margin-top: var(--spacer-12);
}

.privacy-section p {
 font-size: var(--body-md-font-size);
 line-height: var(--body-md-line-height);
 color: var(--text-secondary);
 margin-bottom: var(--spacer-8);
}

.privacy-section ol,
.privacy-section ul {
 padding-left: var(--spacer-16);
 margin-bottom: var(--spacer-8);
}

.privacy-section ol li,
.privacy-section ul li {
 font-size: var(--body-md-font-size);
 line-height: var(--body-md-line-height);
 color: var(--text-secondary);
 margin-bottom: var(--spacer-4);
}

.privacy-section ul {
 list-style: disc;
}

.privacy-section ol {
 list-style: decimal;
}

.privacy-section strong {
 color: var(--text-default);
}
</style>
