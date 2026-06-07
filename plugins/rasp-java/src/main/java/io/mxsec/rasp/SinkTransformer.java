package io.mxsec.rasp;

import java.lang.instrument.ClassFileTransformer;
import java.security.ProtectionDomain;

/**
 * 字节码转换器 (P4-1 骨架).
 *
 * 完整实现 (Sprint 5 单独 PR) 用 ASM 5/9.x 注入字节码:
 *
 *   - java.lang.Runtime.exec*       → invokestatic Hooks.onRuntimeExec
 *   - java.lang.ProcessBuilder.start → invokestatic Hooks.onProcessBuilderStart
 *   - javax.naming.InitialContext.lookup → invokestatic Hooks.onJNDILookup
 *   - java.io.ObjectInputStream.readObject → invokestatic Hooks.onDeserialize
 *   - java.lang.ClassLoader.defineClass → invokestatic Hooks.onDefineClass
 *   - org.apache.catalina.core.StandardContext.addFilterDef → Hooks.onFilterRegistered
 *
 * 当前骨架: 仅注册 transformer + 跳过 boot/RASP 自身, 不真实修改字节码.
 * 完整实现需依赖 asm-9.x.jar 走 ClassVisitor + MethodVisitor.
 */
public class SinkTransformer implements ClassFileTransformer {

    private final RaspConfig config;
    private final java.util.Set<String> targetClasses;

    public SinkTransformer(RaspConfig config) {
        this.config = config;
        this.targetClasses = new java.util.HashSet<>();
        targetClasses.add("java/lang/Runtime");
        targetClasses.add("java/lang/ProcessBuilder");
        targetClasses.add("javax/naming/InitialContext");
        targetClasses.add("java/io/ObjectInputStream");
        targetClasses.add("java/lang/ClassLoader");
        targetClasses.add("org/apache/catalina/core/StandardContext");
        targetClasses.add("org/apache/catalina/core/ApplicationContext");
    }

    @Override
    public byte[] transform(ClassLoader loader, String className,
                            Class<?> classBeingRedefined, ProtectionDomain protectionDomain,
                            byte[] classfileBuffer) {
        if (className == null) return null;
        // 跳过 RASP 自身 + 不在 hook 列表的类
        if (className.startsWith("io/mxsec/rasp/")) return null;
        if (!targetClasses.contains(className)) return null;

        // TODO: Sprint 5 ASM 字节码注入
        //   ClassReader cr = new ClassReader(classfileBuffer);
        //   ClassWriter cw = new ClassWriter(cr, ClassWriter.COMPUTE_FRAMES);
        //   cr.accept(new HookInjector(cw, className), ClassReader.EXPAND_FRAMES);
        //   return cw.toByteArray();

        return null; // 不修改, 走原字节码
    }
}
